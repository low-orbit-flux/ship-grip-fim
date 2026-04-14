package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

// schedJob is one persisted scheduled entry.
type schedJob struct {
	EntryID  cron.EntryID
	Name     string
	Schedule string // cron expression or @daily / @hourly / @weekly / @monthly
	Command  string // "scan" (only command supported for now)
}

// jobRun is one record in the in-memory run history.
type jobRun struct {
	Name      string
	StartTime time.Time
	EndTime   time.Time
	Status    string // "running" | "complete" | "skipped" | "error: …"
}

const maxHistory = 100

// agentScheduler owns the cron engine and all schedule state.
type agentScheduler struct {
	mu      sync.Mutex
	cr      *cron.Cron
	jobs    []schedJob
	history []jobRun
	cfgPath string
	config  configInfo
}

// sched is the package-level scheduler; initialised by startAgentServer.
var sched *agentScheduler

// initScheduler creates the scheduler, loads persisted jobs, and starts the cron engine.
func initScheduler(config configInfo) {
	sched = &agentScheduler{
		cr:      cron.New(),
		cfgPath: config.scheduleConfig,
		config:  config,
	}
	sched.loadFromFile()
	sched.cr.Start()
	fmt.Printf("Scheduler started (%d jobs loaded from %s)\n", len(sched.jobs), config.scheduleConfig)
}

// stopScheduler halts the cron engine gracefully.
func stopScheduler() {
	if sched != nil {
		sched.cr.Stop()
		fmt.Println("Scheduler stopped")
	}
}

// ── persistence ──────────────────────────────────────────────────────────────

func (s *agentScheduler) loadFromFile() {
	f, err := os.Open(s.cfgPath)
	if err != nil {
		return // no file is fine on first run
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "|", 3)
		if len(parts) < 3 {
			fmt.Println("WARN - skipping malformed schedule line:", line)
			continue
		}
		name := strings.TrimSpace(parts[0])
		schedule := strings.TrimSpace(parts[1])
		command := strings.TrimSpace(parts[2])
		if err := s.addJobLocked(name, schedule, command); err != nil {
			fmt.Printf("WARN - could not load job %q: %v\n", name, err)
		}
	}
}

// save writes the current job list to the schedule config file.
// Must NOT be called while s.mu is held.
func (s *agentScheduler) save() {
	s.mu.Lock()
	snapshot := append([]schedJob(nil), s.jobs...)
	s.mu.Unlock()

	f, err := os.Create(s.cfgPath)
	if err != nil {
		fmt.Println("ERROR - saving schedule:", err)
		return
	}
	defer f.Close()

	fmt.Fprintf(f, "# ship-grip-fim schedule configuration\n")
	fmt.Fprintf(f, "# format: name|cronExpression|command\n")
	fmt.Fprintf(f, "# Examples:\n")
	fmt.Fprintf(f, "#   daily_scan|@daily|scan\n")
	fmt.Fprintf(f, "#   nightly|0 2 * * *|scan\n\n")
	for _, j := range snapshot {
		fmt.Fprintf(f, "%s|%s|%s\n", j.Name, j.Schedule, j.Command)
	}
}

// ── job management ────────────────────────────────────────────────────────────

// addJobLocked registers one job with the cron engine and appends to s.jobs.
// Safe to call without holding s.mu (acquires it internally when appending).
func (s *agentScheduler) addJobLocked(name, schedule, command string) error {
	entryID, err := s.cr.AddFunc(schedule, func() {
		s.runJob(name, command)
	})
	if err != nil {
		return fmt.Errorf("invalid schedule %q: %w", schedule, err)
	}
	s.mu.Lock()
	s.jobs = append(s.jobs, schedJob{
		EntryID:  entryID,
		Name:     name,
		Schedule: schedule,
		Command:  command,
	})
	s.mu.Unlock()
	return nil
}

// AddJob adds a new job, validates it, and persists the schedule.
func (s *agentScheduler) AddJob(name, schedule, command string) error {
	// Validate command
	switch command {
	case "scan":
		// ok
	default:
		return fmt.Errorf("unsupported command %q (only 'scan' is supported)", command)
	}

	// Check for duplicate name
	s.mu.Lock()
	for _, j := range s.jobs {
		if j.Name == name {
			s.mu.Unlock()
			return fmt.Errorf("job %q already exists", name)
		}
	}
	s.mu.Unlock()

	if err := s.addJobLocked(name, schedule, command); err != nil {
		return err
	}
	s.save()
	fmt.Printf("[scheduler] Added job %q (%s %s)\n", name, schedule, command)
	return nil
}

// RemoveJob unregisters a job and persists the updated schedule.
func (s *agentScheduler) RemoveJob(name string) error {
	s.mu.Lock()
	idx := -1
	var entryID cron.EntryID
	for i, j := range s.jobs {
		if j.Name == name {
			idx = i
			entryID = j.EntryID
			break
		}
	}
	if idx == -1 {
		s.mu.Unlock()
		return fmt.Errorf("job %q not found", name)
	}
	s.jobs = append(s.jobs[:idx], s.jobs[idx+1:]...)
	s.mu.Unlock()

	s.cr.Remove(entryID) // cron.Cron is internally thread-safe
	s.save()
	fmt.Printf("[scheduler] Removed job %q\n", name)
	return nil
}

// ── job execution ─────────────────────────────────────────────────────────────

func (s *agentScheduler) runJob(name, command string) {
	// Skip if any operation is already running.
	state.mu.Lock()
	if state.scanRunning || state.compareRunning {
		state.mu.Unlock()
		fmt.Printf("[scheduler] Skipping job %q — operation already running\n", name)
		s.recordRun(name, time.Now(), time.Now(), "skipped")
		return
	}
	state.scanRunning = true
	state.mu.Unlock()

	startTime := time.Now()
	s.recordRun(name, startTime, time.Time{}, "running")
	fmt.Printf("[scheduler] Starting job %q (%s)\n", name, command)

	var runStatus string
	func() {
		defer func() {
			if r := recover(); r != nil {
				runStatus = fmt.Sprintf("error: %v", r)
			}
		}()
		callScan(s.config)
		runStatus = "complete"
	}()

	endTime := time.Now()
	state.mu.Lock()
	state.scanRunning = false
	state.mu.Unlock()

	s.updateLastRun(name, startTime, endTime, runStatus)
	fmt.Printf("[scheduler] Job %q finished: %s\n", name, runStatus)
}

// recordRun appends (or updates) a history entry.
func (s *agentScheduler) recordRun(name string, start, end time.Time, status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history = append(s.history, jobRun{
		Name:      name,
		StartTime: start,
		EndTime:   end,
		Status:    status,
	})
	if len(s.history) > maxHistory {
		s.history = s.history[len(s.history)-maxHistory:]
	}
}

// updateLastRun finds the most recent "running" entry for name+startTime and
// fills in the end time and final status.
func (s *agentScheduler) updateLastRun(name string, start, end time.Time, status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.history) - 1; i >= 0; i-- {
		r := &s.history[i]
		if r.Name == name && r.StartTime.Equal(start) && r.Status == "running" {
			r.EndTime = end
			r.Status = status
			return
		}
	}
}

// ── query methods ─────────────────────────────────────────────────────────────

// ListJobs returns a formatted table of all scheduled jobs.
func (s *agentScheduler) ListJobs() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.jobs) == 0 {
		return "No jobs scheduled\n"
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%-20s %-22s %-8s %s\n", "NAME", "SCHEDULE", "COMMAND", "NEXT_RUN"))
	sb.WriteString(strings.Repeat("-", 80) + "\n")
	for _, j := range s.jobs {
		entry := s.cr.Entry(j.EntryID)
		next := "-"
		if !entry.Next.IsZero() {
			next = entry.Next.Format("2006-01-02 15:04:05")
		}
		sb.WriteString(fmt.Sprintf("%-20s %-22s %-8s %s\n", j.Name, j.Schedule, j.Command, next))
	}
	return sb.String()
}

// ListHistory returns the recent job run history (newest first).
func (s *agentScheduler) ListHistory() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.history) == 0 {
		return "No job history\n"
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%-20s %-20s %-20s %s\n", "NAME", "START", "END", "STATUS"))
	sb.WriteString(strings.Repeat("-", 85) + "\n")
	for i := len(s.history) - 1; i >= 0; i-- {
		r := s.history[i]
		end := "-"
		if !r.EndTime.IsZero() && r.Status != "running" {
			end = r.EndTime.Format("2006-01-02 15:04:05")
		}
		sb.WriteString(fmt.Sprintf("%-20s %-20s %-20s %s\n",
			r.Name,
			r.StartTime.Format("2006-01-02 15:04:05"),
			end,
			r.Status,
		))
	}
	return sb.String()
}

// JobsOverview returns a combined status/schedule/history summary.
func (s *agentScheduler) JobsOverview() string {
	state.mu.Lock()
	sr := state.scanRunning
	cr := state.compareRunning
	state.mu.Unlock()

	var sb strings.Builder
	sb.WriteString("=== Running ===\n")
	switch {
	case sr:
		sb.WriteString("scan running\n")
	case cr:
		sb.WriteString("compare running\n")
	default:
		sb.WriteString("idle\n")
	}
	sb.WriteString("\n=== Scheduled Jobs ===\n")
	sb.WriteString(s.ListJobs())
	sb.WriteString("\n=== Recent History ===\n")
	sb.WriteString(s.ListHistory())
	return sb.String()
}

// ── snapshot for GUI ──────────────────────────────────────────────────────────

// ScheduledJobsSnapshot returns a copy of the current job list for the GUI.
func (s *agentScheduler) ScheduledJobsSnapshot() []schedJob {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]schedJob, len(s.jobs))
	copy(out, s.jobs)
	return out
}

// HistorySnapshot returns a copy of the recent history for the GUI.
func (s *agentScheduler) HistorySnapshot() []jobRun {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]jobRun, len(s.history))
	copy(out, s.history)
	return out
}

// NextRunFor returns the next scheduled run time for a named job.
func (s *agentScheduler) NextRunFor(name string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, j := range s.jobs {
		if j.Name == name {
			entry := s.cr.Entry(j.EntryID)
			if entry.Next.IsZero() {
				return "-"
			}
			return entry.Next.Format("2006-01-02 15:04:05")
		}
	}
	return "-"
}
