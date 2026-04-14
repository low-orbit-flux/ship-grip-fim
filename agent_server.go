package main

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

// scanStats records stats from the most recent scan (in-memory; persisted on
// startup via initAgentState by reading the report directory).
type scanStats struct {
	ReportName string
	Timestamp  time.Time
	FileCount  int
	Duration   time.Duration
}

// compareStats records stats from the most recent compare operation.
type compareStats struct {
	OldReport string
	NewReport string
	Timestamp time.Time
	Changed   int
	Added     int
	Missing   int
	Moved     int
}

// agentState tracks whether a long-running operation is in progress and
// caches stats from the most recent scan and compare for the metrics endpoint.
type agentState struct {
	mu             sync.Mutex
	scanRunning    bool
	compareRunning bool
	lastScan       *scanStats
	lastCompare    *compareStats
}

var state = agentState{}

// clientConn wraps a net.Conn with a write mutex so background goroutines
// can safely send async notifications (e.g. "Scan Complete") while the
// read loop is still active.
type clientConn struct {
	conn net.Conn
	mu   sync.Mutex
}

func (cc *clientConn) write(s string) {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	cc.conn.Write([]byte(s)) //nolint:errcheck — best-effort; client may have disconnected
}

// done sends the protocol sentinel that tells the remote CLI it can stop reading.
func (cc *clientConn) done() { cc.write("---DONE---\n") }

// -------------------------------------------------------------------

func startAgentServer(config configInfo) {
	startAgentServerWithStop(config, nil)
}

// startAgentServerWithStop is like startAgentServer but closes the listener
// when the stop channel is closed, allowing a clean shutdown (used by GUI).
func startAgentServerWithStop(config configInfo, stop <-chan struct{}) {
	initAgentState(config)
	initScheduler(config)
	defer stopScheduler()

	addr := config.agentHost + ":" + config.agentPort
	fmt.Println("Agent listening on " + addr)
	l, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Println("ERROR - could not start agent:", err)
		return
	}
	defer l.Close()

	if stop != nil {
		go func() { <-stop; l.Close() }()
	}

	for {
		c, err := l.Accept()
		if err != nil {
			if stop != nil {
				select {
				case <-stop:
					return
				default:
				}
			}
			fmt.Println("ERROR accepting connection:", err)
			return
		}
		fmt.Println("Client connected:", c.RemoteAddr().String())
		go handleConnection(config, &clientConn{conn: c})
	}
}

func handleConnection(config configInfo, cc *clientConn) {
	defer cc.conn.Close()
	reader := bufio.NewReader(cc.conn)

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println("Client disconnected:", cc.conn.RemoteAddr().String())
			return
		}

		parts := strings.Fields(strings.TrimSpace(line))
		if len(parts) == 0 {
			continue
		}

		cmd := parts[0]
		fmt.Println("Command from", cc.conn.RemoteAddr().String()+":", strings.TrimSpace(line))

		switch cmd {

		case "scan":
			state.mu.Lock()
			if state.scanRunning || state.compareRunning {
				state.mu.Unlock()
				cc.write("ERROR - an operation is already running (use 'status' to check)\n")
				cc.done()
				continue
			}
			state.scanRunning = true
			state.mu.Unlock()

			cc.write("Scan started in background\n")
			go func() {
				callScan(config)
				state.mu.Lock()
				state.scanRunning = false
				state.mu.Unlock()
				fmt.Println("Background scan complete")
				// best-effort: notify the client that started the scan
				cc.write("Scan Complete\n")
				cc.done()
			}()
			// do NOT send ---DONE--- here; the goroutine above sends it when finished

		case "list":
			output := listReports(config)
			cc.write(output)
			cc.done()

		case "data":
			if len(parts) < 2 {
				cc.write("ERROR - usage: data <REPORT_ID>\n")
				cc.done()
				continue
			}
			output := listReportDataString(config, parts[1])
			cc.write(output)
			cc.done()

		// fetch returns the raw report file contents (same as data, used by sync).
		case "fetch":
			if len(parts) < 2 {
				cc.write("ERROR - usage: fetch <REPORT_ID>\n")
				cc.done()
				continue
			}
			output := listReportDataString(config, parts[1])
			cc.write(output)
			cc.done()

		case "compare":
			if len(parts) < 3 {
				cc.write("ERROR - usage: compare <REPORT_ID_1> <REPORT_ID_2>\n")
				cc.done()
				continue
			}
			state.mu.Lock()
			if state.scanRunning || state.compareRunning {
				state.mu.Unlock()
				cc.write("ERROR - an operation is already running (use 'status' to check)\n")
				cc.done()
				continue
			}
			state.compareRunning = true
			state.mu.Unlock()

			id1, id2 := parts[1], parts[2]
			cc.write("Compare started in background\n")
			go func() {
				output := compareReportsString(config, id1, id2)
				state.mu.Lock()
				state.compareRunning = false
				state.mu.Unlock()
				fmt.Println("Background compare complete")
				cc.write(output)
				cc.write("Compare Complete\n")
				cc.done()
			}()

		case "status":
			state.mu.Lock()
			sr := state.scanRunning
			cr := state.compareRunning
			state.mu.Unlock()
			switch {
			case sr:
				cc.write("scan running\n")
			case cr:
				cc.write("compare running\n")
			default:
				cc.write("idle\n")
			}
			cc.done()

		case "metrics":
			cc.write(agentMetricsString(config))
			cc.done()

		case "schedule":
			handleScheduleCmd(cc, parts)

		case "jobs":
			// Combined overview: running state + scheduled jobs + recent history.
			if sched == nil {
				cc.write("Scheduler not running\n")
			} else {
				cc.write(sched.JobsOverview())
			}
			cc.done()

		default:
			cc.write("ERROR - unknown command: " + cmd + "\n")
			cc.write("Available commands: scan, list, data <ID>, fetch <ID>, compare <ID> <ID>,\n")
			cc.write("                   status, jobs,\n")
			cc.write("                   schedule list|history|add <name>|<cron>|<cmd>|remove <name>\n")
			cc.done()
		}
	}
}

// handleScheduleCmd processes all "schedule …" sub-commands.
func handleScheduleCmd(cc *clientConn, parts []string) {
	if sched == nil {
		cc.write("ERROR - scheduler not running\n")
		cc.done()
		return
	}
	if len(parts) < 2 {
		cc.write("usage: schedule list|history|add <name>|<cron>|<cmd>|remove <name>\n")
		cc.done()
		return
	}

	switch parts[1] {

	case "list":
		cc.write(sched.ListJobs())
		cc.done()

	case "history":
		cc.write(sched.ListHistory())
		cc.done()

	case "add":
		// Protocol: "schedule add name|cronExpr|command"
		// The payload after "schedule add " may contain spaces (cron expr has spaces).
		if len(parts) < 3 {
			cc.write("usage: schedule add name|cronExpr|command\n")
			cc.write("  example: schedule add daily_scan|@daily|scan\n")
			cc.write("  example: schedule add nightly|0 2 * * *|scan\n")
			cc.done()
			return
		}
		payload := strings.Join(parts[2:], " ")
		fields := strings.SplitN(payload, "|", 3)
		if len(fields) < 3 {
			cc.write("ERROR - usage: schedule add name|cronExpr|command\n")
			cc.done()
			return
		}
		name := strings.TrimSpace(fields[0])
		cronExpr := strings.TrimSpace(fields[1])
		command := strings.TrimSpace(fields[2])
		if err := sched.AddJob(name, cronExpr, command); err != nil {
			cc.write("ERROR - " + err.Error() + "\n")
		} else {
			cc.write("Job " + name + " scheduled (" + cronExpr + " → " + command + ")\n")
		}
		cc.done()

	case "remove":
		if len(parts) < 3 {
			cc.write("usage: schedule remove <name>\n")
			cc.done()
			return
		}
		name := parts[2]
		if err := sched.RemoveJob(name); err != nil {
			cc.write("ERROR - " + err.Error() + "\n")
		} else {
			cc.write("Job " + name + " removed\n")
		}
		cc.done()

	default:
		cc.write("ERROR - unknown schedule sub-command: " + parts[1] + "\n")
		cc.write("Available: list, history, add, remove\n")
		cc.done()
	}
}
