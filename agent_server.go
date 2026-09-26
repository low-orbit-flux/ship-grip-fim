package main

import (
	"bufio"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

// maxCommandLine bounds the size of one protocol line so a client cannot make
// the agent buffer an unbounded amount of memory.
const maxCommandLine = 64 * 1024

// authTimeout is how long an unauthenticated connection may sit idle.
const authTimeout = 30 * time.Second

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
	cert, certCreated, err := loadOrCreateAgentCert(config.agentCert, config.agentKey)
	if err != nil {
		fmt.Println("ERROR - agent TLS certificate:", err)
		return
	}
	if certCreated {
		fmt.Printf("Generated agent TLS certificate %s (private key %s)\n", config.agentCert, config.agentKey)
	}
	fmt.Println("Agent certificate fingerprint:", certFingerprint(cert.Certificate[0]))

	usersCreated, err := ensureDefaultUser(config.usersDB)
	if err != nil {
		fmt.Println("ERROR - users file:", err)
		return
	}
	if usersCreated {
		fmt.Printf("Created %s with the default user %q / %q\n", config.usersDB, defaultAdminUser, defaultAdminPassword)
	}
	if usingDefaultPassword(config.usersDB) {
		fmt.Printf("WARNING - user %q still has the default password; change it with:\n"+
			"          ship-grip-fim user passwd %s <new-password>\n", defaultAdminUser, defaultAdminUser)
	}

	initAgentState(config)
	initScheduler(config)
	defer stopScheduler()

	addr := config.agentHost + ":" + config.agentPort
	fmt.Println("Agent listening on " + addr + " (TLS 1.3, authentication required)")
	l, err := tls.Listen("tcp", addr, agentTLSConfig(cert))
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
			if errors.Is(err, net.ErrClosed) {
				return // listener closed via stop channel
			}
			fmt.Println("ERROR accepting connection:", err)
			continue // transient error; keep serving
		}
		fmt.Println("Client connected:", c.RemoteAddr().String())
		go handleConnection(config, &clientConn{conn: c})
	}
}

func handleConnection(config configInfo, cc *clientConn) {
	defer cc.conn.Close()
	remote := cc.conn.RemoteAddr().String()
	scanner := bufio.NewScanner(cc.conn)
	scanner.Buffer(make([]byte, 0, 4096), maxCommandLine)

	// The first line must be "auth <user> <password>".  Nothing else is
	// accepted before authentication, and the client gets authTimeout to send it.
	cc.conn.SetReadDeadline(time.Now().Add(authTimeout))
	if !scanner.Scan() {
		fmt.Println("Client disconnected before authenticating:", remote)
		return
	}
	authParts := strings.SplitN(strings.TrimSpace(scanner.Text()), " ", 3)
	if len(authParts) != 3 || authParts[0] != "auth" {
		cc.write("ERROR - authentication required: first line must be 'auth <user> <password>'\n")
		return
	}
	user := authParts[1]
	role, ok := authenticate(config.usersDB, user, authParts[2])
	if !ok {
		fmt.Println("Authentication FAILED for user", user, "from", remote)
		cc.write("ERROR - authentication failed\n")
		return
	}
	cc.conn.SetReadDeadline(time.Time{})
	fmt.Println("Authenticated user", user, "("+role+") from", remote)
	cc.write("OK - authenticated as " + user + " (" + role + ")\n")

	for {
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil && !errors.Is(err, net.ErrClosed) {
				fmt.Println("Client", cc.conn.RemoteAddr().String(), "read error:", err)
			}
			fmt.Println("Client disconnected:", cc.conn.RemoteAddr().String())
			return
		}
		line := scanner.Text()

		parts := strings.Fields(strings.TrimSpace(line))
		if len(parts) == 0 {
			continue
		}

		cmd := parts[0]
		logLine := strings.TrimSpace(line)
		if cmd == "user" && len(parts) > 3 {
			logLine = strings.Join(parts[:3], " ") + " ****" // never log passwords
		}
		fmt.Println("Command from", user+"@"+remote+":", logLine)

		if need := commandRole(parts); !roleAllows(role, need) {
			cc.write("ERROR - permission denied: '" + cmd + "' requires the " + need + " role (you are " + role + ")\n")
			cc.done()
			continue
		}

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
				defer func() {
					// A panic here must not take the whole agent down or leave
					// scanRunning stuck at true.
					if r := recover(); r != nil {
						fmt.Println("ERROR - background scan panicked:", r)
						cc.write(fmt.Sprintf("ERROR - scan failed: %v\n", r))
					}
					state.mu.Lock()
					state.scanRunning = false
					state.mu.Unlock()
					cc.done()
				}()
				if err := callScan(config); err != nil {
					fmt.Println("ERROR - background scan:", err)
					cc.write("ERROR - scan failed: " + err.Error() + "\n")
					return
				}
				fmt.Println("Background scan complete")
				// best-effort: notify the client that started the scan
				cc.write("Scan Complete\n")
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
				defer func() {
					if r := recover(); r != nil {
						fmt.Println("ERROR - background compare panicked:", r)
						cc.write(fmt.Sprintf("ERROR - compare failed: %v\n", r))
					}
					state.mu.Lock()
					state.compareRunning = false
					state.mu.Unlock()
					cc.done()
				}()
				output := compareReportsString(config, id1, id2)
				fmt.Println("Background compare complete")
				cc.write(output)
				cc.write("Compare Complete\n")
			}()

		case "quickcompare":
			// Find the two most recent reports that share config.reportName and compare them.
			output := quickCompareString(config)
			cc.write(output)
			cc.done()

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

		case "user":
			// Manage the users file on this agent (admin), or change own password.
			cc.write(userCommandAs(config.usersDB, parts[1:], user, role))
			cc.done()

		case "whoami":
			cc.write(user + " " + role + "\n")
			cc.done()

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
			cc.write("                   status, jobs, metrics, quickcompare,\n")
			cc.write("                   schedule list|history|add <name>|<cron>|<cmd>|remove <name>,\n")
			cc.write("                   whoami, user list|add <name> <pw> [role]|passwd <name> <pw>|role <name> <role>|remove <name>\n")
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
