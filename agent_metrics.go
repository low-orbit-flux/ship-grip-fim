package main

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"time"
)

// ── startup init ──────────────────────────────────────────────────────────────

// initAgentState seeds state.lastScan and state.lastCompare from the report
// directory so metrics are meaningful even right after an agent restart.
func initAgentState(config configInfo) {
	files, err := ioutil.ReadDir(config.reportDir)
	if err != nil {
		return // report dir missing or unreadable — fine on first run
	}

	var (
		latestScanName string
		latestScanTime time.Time
		latestCmpName  string
		latestCmpTime  time.Time
	)

	for _, f := range files {
		name := f.Name()
		t := parseReportTimestamp(name)
		if t.IsZero() {
			continue
		}
		if strings.HasPrefix(name, "compare__") {
			if t.After(latestCmpTime) {
				latestCmpTime = t
				latestCmpName = name
			}
		} else {
			if t.After(latestScanTime) {
				latestScanTime = t
				latestScanName = name
			}
		}
	}

	if latestScanName != "" {
		fileCount := countReportLines(config.reportDir + "/" + latestScanName)
		state.mu.Lock()
		state.lastScan = &scanStats{
			ReportName: extractReportName(latestScanName),
			Timestamp:  latestScanTime,
			FileCount:  fileCount,
			// Duration not recoverable from file
		}
		state.mu.Unlock()
		fmt.Printf("[metrics] Initialised lastScan from %s (%d files)\n", latestScanName, fileCount)
	}

	if latestCmpName != "" {
		changed, added, missing, moved := parseCompareFileStats(config.reportDir + "/" + latestCmpName)
		state.mu.Lock()
		state.lastCompare = &compareStats{
			Timestamp: latestCmpTime,
			Changed:   changed,
			Added:     added,
			Missing:   missing,
			Moved:     moved,
		}
		state.mu.Unlock()
		fmt.Printf("[metrics] Initialised lastCompare from %s\n", latestCmpName)
	}
}

// ── agent metrics response ─────────────────────────────────────────────────────

// agentMetricsString returns the key=value metrics payload for the "metrics"
// agent command.  The exporter parses this to populate Prometheus metrics.
func agentMetricsString(config configInfo) string {
	// Snapshot state under lock.
	state.mu.Lock()
	scanRunning := state.scanRunning
	compareRunning := state.compareRunning
	var ls *scanStats
	var lc *compareStats
	if state.lastScan != nil {
		cp := *state.lastScan
		ls = &cp
	}
	if state.lastCompare != nil {
		cp := *state.lastCompare
		lc = &cp
	}
	state.mu.Unlock()

	b2i := func(b bool) int {
		if b {
			return 1
		}
		return 0
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("scan_running=%d\n", b2i(scanRunning)))
	sb.WriteString(fmt.Sprintf("compare_running=%d\n", b2i(compareRunning)))

	// Report inventory from disk.
	scanReports := listScanReportNames(config)
	sb.WriteString(fmt.Sprintf("reports_total=%d\n", len(scanReports)))
	if mostRecent := mostRecentReportTime(scanReports); !mostRecent.IsZero() {
		sb.WriteString(fmt.Sprintf("last_report_time=%s\n", mostRecent.UTC().Format(time.RFC3339)))
	}

	// Last scan (from memory, seeded at startup).
	if ls != nil {
		sb.WriteString(fmt.Sprintf("last_scan_report=%s\n", ls.ReportName))
		sb.WriteString(fmt.Sprintf("last_scan_time=%s\n", ls.Timestamp.UTC().Format(time.RFC3339)))
		sb.WriteString(fmt.Sprintf("last_scan_files=%d\n", ls.FileCount))
		sb.WriteString(fmt.Sprintf("last_scan_duration=%.3f\n", ls.Duration.Seconds()))
	}

	// Last compare (from memory, seeded at startup).
	if lc != nil {
		sb.WriteString(fmt.Sprintf("last_compare_time=%s\n", lc.Timestamp.UTC().Format(time.RFC3339)))
		sb.WriteString(fmt.Sprintf("last_compare_changed=%d\n", lc.Changed))
		sb.WriteString(fmt.Sprintf("last_compare_new=%d\n", lc.Added))
		sb.WriteString(fmt.Sprintf("last_compare_missing=%d\n", lc.Missing))
		sb.WriteString(fmt.Sprintf("last_compare_moved=%d\n", lc.Moved))
	}

	// Scheduler state.
	if sched != nil {
		jobs := sched.ScheduledJobsSnapshot()
		history := sched.HistorySnapshot()
		sb.WriteString(fmt.Sprintf("jobs_total=%d\n", len(jobs)))

		for _, j := range jobs {
			next := sched.NextRunFor(j.Name)
			sb.WriteString(fmt.Sprintf("job.%s.next=%s\n", j.Name, next))

			// Find the most recent non-running history entry for this job.
			lastRunStr := "never"
			lastStatus := "never"
			for i := len(history) - 1; i >= 0; i-- {
				r := history[i]
				if r.Name == j.Name && r.Status != "running" {
					lastRunStr = r.StartTime.UTC().Format(time.RFC3339)
					lastStatus = r.Status
					break
				}
			}
			sb.WriteString(fmt.Sprintf("job.%s.last=%s\n", j.Name, lastRunStr))
			sb.WriteString(fmt.Sprintf("job.%s.status=%s\n", j.Name, lastStatus))
		}
	}

	return sb.String()
}

// ── report directory helpers ───────────────────────────────────────────────────

// listScanReportNames returns all filenames in reportDir that are scan reports
// (i.e. do NOT start with "compare__").
func listScanReportNames(config configInfo) []string {
	files, err := ioutil.ReadDir(config.reportDir)
	if err != nil {
		return nil
	}
	var out []string
	for _, f := range files {
		if !strings.HasPrefix(f.Name(), "compare__") {
			out = append(out, f.Name())
		}
	}
	return out
}

// mostRecentReportTime returns the latest timestamp found across the given
// report filenames (returns zero time if none are parseable).
func mostRecentReportTime(names []string) time.Time {
	var latest time.Time
	for _, n := range names {
		if t := parseReportTimestamp(n); !t.IsZero() && t.After(latest) {
			latest = t
		}
	}
	return latest
}

// parseReportTimestamp extracts the trailing YYYY-MM-DD_HH:MM:SS from a
// report filename.  Works for both scan reports and compare__ reports.
func parseReportTimestamp(filename string) time.Time {
	parts := strings.Split(filename, "_")
	// Filter empty tokens produced by "__" separators.
	var tok []string
	for _, p := range parts {
		if p != "" {
			tok = append(tok, p)
		}
	}
	if len(tok) < 2 {
		return time.Time{}
	}
	datePart := tok[len(tok)-2]
	timePart := tok[len(tok)-1]
	t, err := time.ParseInLocation("2006-01-02 15:04:05", datePart+" "+timePart, time.Local)
	if err != nil {
		return time.Time{}
	}
	return t
}

// extractReportName strips the trailing timestamp from a scan report filename
// to recover the logical report name prefix.
func extractReportName(filename string) string {
	parts := strings.Split(filename, "_")
	var tok []string
	for _, p := range parts {
		if p != "" {
			tok = append(tok, p)
		}
	}
	if len(tok) < 3 {
		return filename
	}
	// Drop the last two tokens (date and time).
	return strings.Join(tok[:len(tok)-2], "_")
}

// countReportLines counts the data lines in a report file (excludes the header).
func countReportLines(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	count := 0
	s := bufio.NewScanner(f)
	s.Scan() // skip header line
	for s.Scan() {
		if s.Text() != "" {
			count++
		}
	}
	return count
}

// parseCompareFileStats reads a compare report file and returns counts for
// each change category.
func parseCompareFileStats(path string) (changed, added, missing, moved int) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := s.Text()
		switch {
		case strings.HasPrefix(line, "CHANGED - "):
			changed++
		case strings.HasPrefix(line, "NEW - "):
			added++
		case strings.HasPrefix(line, "MISSING - "):
			missing++
		case strings.HasPrefix(line, "MOVED - "):
			moved++
		}
	}
	return
}
