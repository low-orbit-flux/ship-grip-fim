package main

import (
	"fmt"
	"regexp"
	"strings"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// startGUI opens the main window and runs the event loop.
// It recovers from the panic that fyne/glfw raises when no display is available
// and prints a helpful message instead.
func startGUI(config configInfo) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("ERROR - could not open display.")
			fmt.Println("Make sure a graphical environment is available (DISPLAY is set, or use X11 forwarding).")
			fmt.Printf("Detail: %v\n", r)
		}
	}()

	a := app.NewWithID("com.github.low-orbit-flux.ship-grip-fim")
	w := a.NewWindow("ship-grip-fim — File Integrity Monitor")
	w.Resize(fyne.NewSize(1200, 750))

	tabs := container.NewAppTabs(
		container.NewTabItem("Scan & Reports", makeLocalTab(config)),
		container.NewTabItem("Agent", makeAgentTab(config)),
		container.NewTabItem("Remote", makeRemoteTab(config)),
		container.NewTabItem("Hosts", makeHostsTab(config)),
		container.NewTabItem("Schedule", makeScheduleTab(config)),
		container.NewTabItem("Users", makeUsersTab(config)),
	)
	tabs.SetTabLocation(container.TabLocationTop)

	w.SetContent(tabs)
	w.ShowAndRun()
}

// ── helpers ──────────────────────────────────────────────────────────────────

// parseReportList filters the raw text returned by listReports() down to
// valid report filenames (no whitespace, no separator lines).
func parseReportList(raw string) []string {
	var out []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" ||
			strings.ContainsAny(line, " \t") ||
			strings.HasPrefix(line, "-") ||
			strings.HasPrefix(line, "=") {
			continue
		}
		out = append(out, line)
	}
	return out
}

// shorten truncates a long report name for use in compact labels.
func shorten(s string) string {
	if len(s) > 38 {
		return "…" + s[len(s)-37:]
	}
	return s
}

// roEntry returns a MultiLineEntry configured as a read-only display area.
func roEntry() *widget.Entry {
	e := widget.NewMultiLineEntry()
	e.Disable()
	e.Wrapping = fyne.TextWrapBreak
	return e
}

// NOTE on threading: Fyne 2.6+ requires every widget mutation to happen on
// the main goroutine.  Anything that runs in a `go func()` below performs its
// I/O first and then applies the UI changes inside fyne.Do(...).  The helpers
// setText/appendText are plain and must only be called from the main
// goroutine or from inside a fyne.Do callback.

// credEntries returns user/password entries pre-filled from the config; every
// tab that talks to an agent shows them next to host/port.
func credEntries(config configInfo) (*widget.Entry, *widget.Entry) {
	userEntry := widget.NewEntry()
	userEntry.SetText(config.agentUser)
	userEntry.SetPlaceHolder("user")
	passEntry := widget.NewPasswordEntry()
	passEntry.SetText(config.agentPassword)
	passEntry.SetPlaceHolder("password")
	return userEntry, passEntry
}

// setText sets the text of an entry regardless of its disabled state.
func setText(e *widget.Entry, s string) {
	e.Enable()
	e.SetText(s)
	e.Disable()
}

// appendText appends text to an entry display area.
func appendText(e *widget.Entry, s string) {
	setText(e, e.Text+s)
}

// ── Tab 1: Scan & Reports (local) ────────────────────────────────────────────

func makeLocalTab(config configInfo) fyne.CanvasObject {
	// ── config entries ──
	pathEntry := widget.NewEntry()
	pathEntry.SetText(config.path)
	pathEntry.SetPlaceHolder("directory to scan")

	reportNameEntry := widget.NewEntry()
	reportNameEntry.SetText(config.reportName)
	reportNameEntry.SetPlaceHolder("report name prefix")

	reportDirEntry := widget.NewEntry()
	reportDirEntry.SetText(config.reportDir)
	reportDirEntry.SetPlaceHolder("report storage directory")

	// ── output area ──
	output := roEntry()

	// ── report list ──
	var (
		reports        []string
		selectedReport string
		compareOld     string
		compareNew     string
	)

	statusLabel := widget.NewLabel("Ready")
	compareOldLabel := widget.NewLabel("Old: (none)")
	compareNewLabel := widget.NewLabel("New: (none)")

	reportList := widget.NewList(
		func() int { return len(reports) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.ListItemID, o fyne.CanvasObject) {
			o.(*widget.Label).SetText(reports[id])
		},
	)
	reportList.OnSelected = func(id widget.ListItemID) {
		if id < len(reports) {
			selectedReport = reports[id]
		}
	}

	// currentConfig builds a config snapshot from the current entry values.
	currentConfig := func() configInfo {
		cfg := config
		cfg.path = pathEntry.Text
		cfg.reportName = reportNameEntry.Text
		cfg.reportDir = reportDirEntry.Text
		return cfg
	}

	// refreshList must be called on the main goroutine.
	refreshList := func() {
		reports = parseReportList(listReports(currentConfig()))
		reportList.Refresh()
	}

	// ── action buttons ──
	scanBtn := widget.NewButton("▶  Scan", func() {
		statusLabel.SetText("Scanning…")
		cfg := currentConfig()
		go func() {
			msg := "Scan complete"
			if err := callScan(cfg); err != nil {
				msg = "Scan failed: " + err.Error()
			}
			newReports := parseReportList(listReports(cfg))
			fyne.Do(func() {
				reports = newReports
				reportList.Refresh()
				statusLabel.SetText(msg)
			})
		}()
	})
	scanBtn.Importance = widget.HighImportance

	quickCompareBtn := widget.NewButton("⚡  Quick Compare", func() {
		statusLabel.SetText("Comparing last two reports…")
		cfg := currentConfig()
		go func() {
			result := quickCompareString(cfg)
			fyne.Do(func() {
				setText(output, result)
				statusLabel.SetText("Quick compare complete")
			})
		}()
	})
	quickCompareBtn.Importance = widget.WarningImportance

	refreshBtn := widget.NewButton("⟳  Refresh List", func() {
		refreshList()
		statusLabel.SetText("List refreshed")
	})

	viewBtn := widget.NewButton("View Data", func() {
		if selectedReport == "" {
			setText(output, "Select a report from the list first.")
			return
		}
		setText(output, listReportDataString(currentConfig(), selectedReport))
	})

	setOldBtn := widget.NewButton("Set as Old", func() {
		if selectedReport == "" {
			return
		}
		compareOld = selectedReport
		compareOldLabel.SetText("Old: " + shorten(compareOld))
	})

	setNewBtn := widget.NewButton("Set as New", func() {
		if selectedReport == "" {
			return
		}
		compareNew = selectedReport
		compareNewLabel.SetText("New: " + shorten(compareNew))
	})

	compareBtn := widget.NewButton("Compare", func() {
		if compareOld == "" || compareNew == "" {
			setText(output, "Set both Old and New reports before comparing.")
			return
		}
		statusLabel.SetText("Comparing…")
		cfg := currentConfig()
		oldID, newID := compareOld, compareNew
		go func() {
			result := compareReportsString(cfg, oldID, newID)
			fyne.Do(func() {
				setText(output, result)
				statusLabel.SetText("Compare complete — result saved to reportDir")
			})
		}()
	})
	compareBtn.Importance = widget.WarningImportance

	clearBtn := widget.NewButton("Clear", func() { setText(output, "") })

	// ── layout ──
	configPanel := container.NewVBox(
		container.NewGridWithColumns(2,
			widget.NewLabel("Scan Path:"), pathEntry,
			widget.NewLabel("Report Name:"), reportNameEntry,
			widget.NewLabel("Report Dir:"), reportDirEntry,
		),
		container.NewHBox(scanBtn, quickCompareBtn, refreshBtn, widget.NewSeparator(), statusLabel),
	)

	actionBar := container.NewHBox(
		viewBtn,
		widget.NewSeparator(),
		setOldBtn, compareOldLabel,
		setNewBtn, compareNewLabel,
		compareBtn,
		widget.NewSeparator(),
		clearBtn,
	)

	split := container.NewHSplit(
		container.NewBorder(
			widget.NewLabelWithStyle("Reports", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			nil, nil, nil, reportList,
		),
		container.NewScroll(output),
	)
	split.SetOffset(0.28)

	refreshList() // populate on load

	return container.NewBorder(configPanel, actionBar, nil, nil, split)
}

// ── Tab 2: Agent ─────────────────────────────────────────────────────────────

func makeAgentTab(config configInfo) fyne.CanvasObject {
	agentHostEntry := widget.NewEntry()
	agentHostEntry.SetText(config.agentHost)
	agentHostEntry.SetPlaceHolder("bind address")

	agentPortEntry := widget.NewEntry()
	agentPortEntry.SetText(config.agentPort)
	agentPortEntry.SetPlaceHolder("port")

	statusLabel := widget.NewLabel("Agent: stopped")
	logOutput := roEntry()

	var (
		mu     sync.Mutex
		stopCh chan struct{}
	)

	startBtn := widget.NewButton("▶  Start Agent", nil)
	stopBtn := widget.NewButton("■  Stop Agent", nil)
	stopBtn.Disable()
	startBtn.Importance = widget.HighImportance

	startBtn.OnTapped = func() {
		mu.Lock()
		defer mu.Unlock()
		if stopCh != nil {
			return // already running
		}

		cfg := config
		cfg.agentHost = agentHostEntry.Text
		cfg.agentPort = agentPortEntry.Text
		stopCh = make(chan struct{})
		sc := stopCh

		startBtn.Disable()
		stopBtn.Enable()
		agentHostEntry.Disable()
		agentPortEntry.Disable()
		statusLabel.SetText(fmt.Sprintf("Agent: running on %s:%s", cfg.agentHost, cfg.agentPort))
		appendText(logOutput, fmt.Sprintf("Started on %s:%s\n", cfg.agentHost, cfg.agentPort))

		go func() {
			startAgentServerWithStop(cfg, sc)
			mu.Lock()
			stopCh = nil
			mu.Unlock()
			fyne.Do(func() {
				statusLabel.SetText("Agent: stopped")
				startBtn.Enable()
				stopBtn.Disable()
				agentHostEntry.Enable()
				agentPortEntry.Enable()
				appendText(logOutput, "Agent stopped\n")
			})
		}()
	}

	stopBtn.OnTapped = func() {
		mu.Lock()
		defer mu.Unlock()
		if stopCh != nil {
			close(stopCh)
		}
	}

	clearLogBtn := widget.NewButton("Clear Log", func() { setText(logOutput, "") })

	topBar := container.NewHBox(
		widget.NewLabel("Bind:"), agentHostEntry,
		widget.NewLabel("Port:"), agentPortEntry,
		startBtn, stopBtn,
		widget.NewSeparator(), statusLabel,
		widget.NewSeparator(), clearLogBtn,
	)

	info := widget.NewLabel(
		"The agent accepts TCP connections from 'remote' and 'sync' commands.\n" +
			"Set Bind to 0.0.0.0 to accept connections from other machines.",
	)

	return container.NewBorder(
		container.NewVBox(topBar, info),
		nil, nil, nil,
		container.NewScroll(logOutput),
	)
}

// ── Tab 3: Remote ─────────────────────────────────────────────────────────────

func makeRemoteTab(config configInfo) fyne.CanvasObject {
	hostEntry := widget.NewEntry()
	hostEntry.SetText(config.agentHost)
	hostEntry.SetPlaceHolder("agent host")

	portEntry := widget.NewEntry()
	portEntry.SetText(config.agentPort)
	portEntry.SetPlaceHolder("port")

	output := roEntry()

	var (
		remoteReports  []string
		selectedRemote string
		compareOld     string
		compareNew     string
	)

	statusLabel := widget.NewLabel("Idle")
	compareOldLabel := widget.NewLabel("Old: (none)")
	compareNewLabel := widget.NewLabel("New: (none)")

	remoteList := widget.NewList(
		func() int { return len(remoteReports) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.ListItemID, o fyne.CanvasObject) {
			o.(*widget.Label).SetText(remoteReports[id])
		},
	)
	remoteList.OnSelected = func(id widget.ListItemID) {
		if id < len(remoteReports) {
			selectedRemote = remoteReports[id]
		}
	}

	userEntry, passEntry := credEntries(config)
	addr := func() (configInfo, string, string) {
		cfg := config
		cfg.agentUser = userEntry.Text
		cfg.agentPassword = passEntry.Text
		return cfg, hostEntry.Text, portEntry.Text
	}

	statusBtn := widget.NewButton("Status", func() {
		cfg, h, p := addr()
		statusLabel.SetText("Checking…")
		go func() {
			result := runRemoteCommandToString(cfg, h, p, []string{"status"})
			fyne.Do(func() { statusLabel.SetText(strings.TrimSpace(result)) })
		}()
	})

	listBtn := widget.NewButton("⟳  List Reports", func() {
		cfg, h, p := addr()
		statusLabel.SetText("Listing…")
		go func() {
			raw := runRemoteCommandToString(cfg, h, p, []string{"list"})
			list := parseReportList(raw)
			fyne.Do(func() {
				remoteReports = list
				remoteList.Refresh()
				statusLabel.SetText(fmt.Sprintf("%d reports", len(remoteReports)))
			})
		}()
	})

	scanBtn := widget.NewButton("▶  Scan", func() {
		cfg, h, p := addr()
		statusLabel.SetText("Scan running on remote…")
		go func() {
			result := runRemoteCommandToString(cfg, h, p, []string{"scan"})
			fyne.Do(func() {
				setText(output, result)
				statusLabel.SetText("Scan complete")
			})
		}()
	})
	scanBtn.Importance = widget.HighImportance

	remoteQCBtn := widget.NewButton("⚡  Quick Compare", func() {
		cfg, h, p := addr()
		statusLabel.SetText("Quick compare on remote…")
		go func() {
			result := runRemoteCommandToString(cfg, h, p, []string{"quickcompare"})
			fyne.Do(func() {
				setText(output, result)
				statusLabel.SetText("Quick compare complete")
			})
		}()
	})
	remoteQCBtn.Importance = widget.WarningImportance

	viewBtn := widget.NewButton("View Data", func() {
		if selectedRemote == "" {
			setText(output, "Select a report first.")
			return
		}
		cfg, h, p := addr()
		id := selectedRemote
		go func() {
			result := runRemoteCommandToString(cfg, h, p, []string{"data", id})
			fyne.Do(func() { setText(output, result) })
		}()
	})

	setOldBtn := widget.NewButton("Set as Old", func() {
		if selectedRemote == "" {
			return
		}
		compareOld = selectedRemote
		compareOldLabel.SetText("Old: " + shorten(compareOld))
	})

	setNewBtn := widget.NewButton("Set as New", func() {
		if selectedRemote == "" {
			return
		}
		compareNew = selectedRemote
		compareNewLabel.SetText("New: " + shorten(compareNew))
	})

	compareBtn := widget.NewButton("Compare", func() {
		if compareOld == "" || compareNew == "" {
			setText(output, "Set both Old and New reports before comparing.")
			return
		}
		cfg, h, p := addr()
		statusLabel.SetText("Comparing on remote…")
		oldID, newID := compareOld, compareNew
		go func() {
			result := runRemoteCommandToString(cfg, h, p, []string{"compare", oldID, newID})
			fyne.Do(func() {
				setText(output, result)
				statusLabel.SetText("Compare complete")
			})
		}()
	})
	compareBtn.Importance = widget.WarningImportance

	clearBtn := widget.NewButton("Clear", func() { setText(output, "") })

	topBar := container.NewHBox(
		widget.NewLabel("Host:"), hostEntry,
		widget.NewLabel("Port:"), portEntry,
		widget.NewLabel("User:"), userEntry,
		widget.NewLabel("Pass:"), passEntry,
		statusBtn, listBtn, scanBtn, remoteQCBtn,
		widget.NewSeparator(), statusLabel,
	)

	actionBar := container.NewHBox(
		viewBtn,
		widget.NewSeparator(),
		setOldBtn, compareOldLabel,
		setNewBtn, compareNewLabel,
		compareBtn,
		widget.NewSeparator(),
		clearBtn,
	)

	split := container.NewHSplit(
		container.NewBorder(
			widget.NewLabelWithStyle("Remote Reports", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			nil, nil, nil, remoteList,
		),
		container.NewScroll(output),
	)
	split.SetOffset(0.28)

	return container.NewBorder(topBar, actionBar, nil, nil, split)
}

// ── Tab 4: Hosts ──────────────────────────────────────────────────────────────

func makeHostsTab(config configInfo) fyne.CanvasObject {
	hostsConfigEntry := widget.NewEntry()
	hostsConfigEntry.SetText(config.hostsConfig)
	hostsConfigEntry.SetPlaceHolder("hosts.conf path")

	output := roEntry()
	statusLabel := widget.NewLabel("No hosts loaded")

	var (
		hostsMu sync.Mutex
		hosts   []remoteHost
	)

	// ── hosts table ──
	// Row 0 is the header; data rows start at 1.
	const numCols = 7
	colHeaders := []string{"Alias", "Address", "Port", "Path", "Report Name", "SSH User", "Binary Path"}
	colWidths := []float32{120, 160, 60, 200, 160, 100, 220}

	hostsTable := widget.NewTable(
		func() (int, int) {
			hostsMu.Lock()
			defer hostsMu.Unlock()
			return len(hosts) + 1, numCols
		},
		func() fyne.CanvasObject {
			return widget.NewLabel("")
		},
		func(id widget.TableCellID, o fyne.CanvasObject) {
			lbl := o.(*widget.Label)
			if id.Row == 0 {
				lbl.TextStyle = fyne.TextStyle{Bold: true}
				lbl.SetText(colHeaders[id.Col])
				return
			}
			lbl.TextStyle = fyne.TextStyle{}
			hostsMu.Lock()
			h := hosts[id.Row-1]
			hostsMu.Unlock()
			vals := []string{h.alias, h.address, h.port, h.path, h.reportName, h.sshUser, h.binaryPath}
			lbl.SetText(vals[id.Col])
		},
	)
	for i, w := range colWidths {
		hostsTable.SetColumnWidth(i, w)
	}

	// Clicking a row selects that host for the "Selected" SSH actions.
	var selectedAlias string
	hostsTable.OnSelected = func(id widget.TableCellID) {
		if id.Row == 0 {
			return
		}
		hostsMu.Lock()
		if id.Row-1 < len(hosts) {
			selectedAlias = hosts[id.Row-1].alias
		}
		hostsMu.Unlock()
		statusLabel.SetText("Selected: " + selectedAlias)
	}

	currentCfg := func() configInfo {
		cfg := config
		cfg.hostsConfig = hostsConfigEntry.Text
		return cfg
	}

	loadHosts := func() {
		cfg := currentCfg()
		loaded, err := parseHostsConfig(cfg.hostsConfig)
		if err != nil {
			setText(output, "ERROR loading hosts: "+err.Error())
			statusLabel.SetText("Load error")
			return
		}
		hostsMu.Lock()
		hosts = loaded
		hostsMu.Unlock()
		hostsTable.Refresh()
		statusLabel.SetText(fmt.Sprintf("%d hosts loaded", len(loaded)))
	}

	loadBtn := widget.NewButton("Load", loadHosts)
	loadBtn.Importance = widget.HighImportance

	// ── ping all ──
	pingAllBtn := widget.NewButton("Ping All", func() {
		hostsMu.Lock()
		snapshot := append([]remoteHost(nil), hosts...)
		hostsMu.Unlock()
		if len(snapshot) == 0 {
			setText(output, "No hosts loaded.")
			return
		}
		cfg := currentCfg()
		statusLabel.SetText("Pinging…")
		go func() {
			results := make([]hostResult, len(snapshot))
			var wg sync.WaitGroup
			for i, h := range snapshot {
				wg.Add(1)
				go func(idx int, host remoteHost) {
					defer wg.Done()
					out := runRemoteCommandToString(hostConfig(cfg, host), host.address, host.port, []string{"status"})
					results[idx] = hostResult{alias: host.alias, output: strings.TrimSpace(out)}
				}(i, h)
			}
			wg.Wait()

			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("%-18s %s\n", "ALIAS", "STATUS"))
			sb.WriteString(strings.Repeat("-", 45) + "\n")
			for _, r := range results {
				sb.WriteString(fmt.Sprintf("%-18s %s\n", r.alias, r.output))
			}
			fyne.Do(func() {
				setText(output, sb.String())
				statusLabel.SetText("Ping complete")
			})
		}()
	})

	// ── sync all ──
	syncAllBtn := widget.NewButton("Sync Reports", func() {
		hostsMu.Lock()
		snapshot := append([]remoteHost(nil), hosts...)
		hostsMu.Unlock()
		if len(snapshot) == 0 {
			setText(output, "No hosts loaded.")
			return
		}
		cfg := currentCfg()
		statusLabel.SetText("Syncing…")
		setText(output, "")
		go func() {
			for _, h := range snapshot {
				alias := h.alias
				fyne.Do(func() { appendText(output, fmt.Sprintf("[%s] syncing…\n", alias)) })
				syncHostReports(cfg, h)
			}
			fyne.Do(func() {
				statusLabel.SetText("Sync complete")
				appendText(output, "\nAll hosts synced.\n")
			})
		}()
	})

	// ── run on all ──
	cmdEntry := widget.NewEntry()
	cmdEntry.SetText("status")
	cmdEntry.SetPlaceHolder("command (e.g. scan, list, status)")

	runAllBtn := widget.NewButton("Run on All", func() {
		hostsMu.Lock()
		snapshot := append([]remoteHost(nil), hosts...)
		hostsMu.Unlock()
		if len(snapshot) == 0 {
			setText(output, "No hosts loaded.")
			return
		}
		cmdArgs := strings.Fields(cmdEntry.Text)
		if len(cmdArgs) == 0 {
			return
		}
		cfg := currentCfg()
		statusLabel.SetText("Running…")
		go func() {
			results := make([]hostResult, len(snapshot))
			var wg sync.WaitGroup
			for i, h := range snapshot {
				wg.Add(1)
				go func(idx int, host remoteHost) {
					defer wg.Done()
					out := runRemoteCommandToString(hostConfig(cfg, host), host.address, host.port, cmdArgs)
					results[idx] = hostResult{alias: host.alias, output: out}
				}(i, h)
			}
			wg.Wait()
			var sb strings.Builder
			for _, r := range results {
				sb.WriteString(fmt.Sprintf("=== %s ===\n%s\n", r.alias, r.output))
			}
			fyne.Do(func() {
				setText(output, sb.String())
				statusLabel.SetText("Done")
			})
		}()
	})

	// ── start / deploy agents via SSH ──
	// Output from the SSH helpers lands in the output box instead of stdout.
	startVia := func(alias string) {
		hostsMu.Lock()
		n := len(hosts)
		hostsMu.Unlock()
		if n == 0 {
			setText(output, "No hosts loaded.")
			return
		}
		cfg := currentCfg()
		statusLabel.SetText("Starting agents…")
		setText(output, "")
		go func() {
			startAgents(cfg, alias, entryWriter{output})
			fyne.Do(func() { statusLabel.SetText("Start commands sent") })
		}()
	}
	startSelBtn := widget.NewButton("Start Selected (SSH)", func() {
		if selectedAlias == "" {
			setText(output, "Select a host in the table first.")
			return
		}
		startVia(selectedAlias)
	})
	startAllBtn := widget.NewButton("Start All (SSH)", func() { startVia("") })

	binaryEntry := widget.NewEntry()
	binaryEntry.SetText(defaultDeployBinary())
	binaryEntry.SetPlaceHolder("local binary to deploy")
	restartCheck := widget.NewCheck("Restart agent", nil)
	restartCheck.SetChecked(true)

	var deploying bool
	deployVia := func(alias string) {
		hostsMu.Lock()
		snapshot := append([]remoteHost(nil), hosts...)
		hostsMu.Unlock()
		if len(snapshot) == 0 {
			setText(output, "No hosts loaded.")
			return
		}
		if deploying {
			appendText(output, "A deploy is already running.\n")
			return
		}
		deploying = true
		cfg := currentCfg()
		opts := deployOptions{binary: binaryEntry.Text, restart: restartCheck.Checked}
		statusLabel.SetText("Deploying…")
		setText(output, "")
		go func() {
			err := deployHosts(cfg, snapshot, alias, opts, entryWriter{output})
			fyne.Do(func() {
				deploying = false
				if err != nil {
					appendText(output, "ERROR - "+err.Error()+"\n")
					statusLabel.SetText("Deploy failed")
				} else {
					statusLabel.SetText("Deploy complete")
				}
			})
		}()
	}
	deploySelBtn := widget.NewButton("Deploy Selected", func() {
		if selectedAlias == "" {
			setText(output, "Select a host in the table first.")
			return
		}
		deployVia(selectedAlias)
	})
	deployAllBtn := widget.NewButton("Deploy All", func() { deployVia("") })
	deployAllBtn.Importance = widget.HighImportance

	clearBtn := widget.NewButton("Clear", func() { setText(output, "") })

	// ── layout ──
	topBar := container.NewHBox(
		widget.NewLabel("Hosts Config:"), hostsConfigEntry, loadBtn,
		widget.NewSeparator(), statusLabel,
	)

	actionBar := container.NewHBox(
		pingAllBtn, syncAllBtn, startSelBtn, startAllBtn,
		widget.NewSeparator(),
		widget.NewLabel("Cmd:"), cmdEntry, runAllBtn,
		widget.NewSeparator(),
		clearBtn,
	)

	// Deploy over SSH: copies the binary to each host's binaryPath, installs
	// missing config files, pins the agent fingerprint and restarts the agent
	// (the Go version of scripts/deploy.sh).
	deployBar := container.NewBorder(nil, nil,
		widget.NewLabel("Deploy binary:"),
		container.NewHBox(restartCheck, deploySelBtn, deployAllBtn),
		binaryEntry,
	)

	split := container.NewVSplit(
		hostsTable,
		container.NewScroll(output),
	)
	split.SetOffset(0.45)

	loadHosts() // try to load on startup

	return container.NewBorder(topBar, container.NewVBox(actionBar, deployBar), nil, nil, split)
}

// entryWriter is an io.Writer that appends to a read-only entry from any
// goroutine, so background SSH work can stream its progress into the GUI.
type entryWriter struct{ e *widget.Entry }

func (w entryWriter) Write(p []byte) (int, error) {
	s := string(p)
	fyne.Do(func() { appendText(w.e, s) })
	return len(p), nil
}

// ── Tab 5: Schedule ───────────────────────────────────────────────────────────

func makeScheduleTab(config configInfo) fyne.CanvasObject {
	// Connection to the agent that owns the scheduler.
	hostEntry := widget.NewEntry()
	hostEntry.SetText(config.agentHost)
	hostEntry.SetPlaceHolder("agent host")

	portEntry := widget.NewEntry()
	portEntry.SetText(config.agentPort)
	portEntry.SetPlaceHolder("port")

	statusLabel := widget.NewLabel("Not connected")
	output := roEntry()

	userEntry, passEntry := credEntries(config)
	addr := func() (configInfo, string, string) {
		cfg := config
		cfg.agentUser = userEntry.Text
		cfg.agentPassword = passEntry.Text
		return cfg, hostEntry.Text, portEntry.Text
	}

	// ── scheduled jobs table ──
	const jobCols = 4
	jobHeaders := []string{"Name", "Schedule", "Command", "Next Run"}
	jobColWidths := []float32{150, 180, 90, 180}

	var (
		jobsMu  sync.Mutex
		jobRows []schedJobRow
	)

	jobsTable := widget.NewTable(
		func() (int, int) {
			jobsMu.Lock()
			defer jobsMu.Unlock()
			return len(jobRows) + 1, jobCols
		},
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.TableCellID, o fyne.CanvasObject) {
			lbl := o.(*widget.Label)
			if id.Row == 0 {
				lbl.TextStyle = fyne.TextStyle{Bold: true}
				lbl.SetText(jobHeaders[id.Col])
				return
			}
			lbl.TextStyle = fyne.TextStyle{}
			jobsMu.Lock()
			if id.Row-1 < len(jobRows) {
				r := jobRows[id.Row-1]
				vals := []string{r.name, r.schedule, r.command, r.next}
				lbl.SetText(vals[id.Col])
			}
			jobsMu.Unlock()
		},
	)
	for i, w := range jobColWidths {
		jobsTable.SetColumnWidth(i, w)
	}

	var selectedJobName string
	jobsTable.OnSelected = func(id widget.TableCellID) {
		if id.Row == 0 {
			return
		}
		jobsMu.Lock()
		if id.Row-1 < len(jobRows) {
			selectedJobName = jobRows[id.Row-1].name
		}
		jobsMu.Unlock()
	}

	// ── history table ──
	const histCols = 4
	histHeaders := []string{"Name", "Start", "End", "Status"}
	histColWidths := []float32{150, 180, 180, 160}

	var (
		histMu   sync.Mutex
		histRows []histJobRow
	)

	histTable := widget.NewTable(
		func() (int, int) {
			histMu.Lock()
			defer histMu.Unlock()
			return len(histRows) + 1, histCols
		},
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.TableCellID, o fyne.CanvasObject) {
			lbl := o.(*widget.Label)
			if id.Row == 0 {
				lbl.TextStyle = fyne.TextStyle{Bold: true}
				lbl.SetText(histHeaders[id.Col])
				return
			}
			lbl.TextStyle = fyne.TextStyle{}
			histMu.Lock()
			if id.Row-1 < len(histRows) {
				r := histRows[id.Row-1]
				vals := []string{r.name, r.start, r.end, r.status}
				lbl.SetText(vals[id.Col])
			}
			histMu.Unlock()
		},
	)
	for i, w := range histColWidths {
		histTable.SetColumnWidth(i, w)
	}

	// ── refresh: fetch schedule list and history from agent ──
	// refreshFrom does the network I/O synchronously in the calling goroutine
	// and hands every UI update to the main goroutine via fyne.Do, so it is
	// safe to call from any background goroutine.
	refreshFrom := func(cfg configInfo, h, p string) {
		fyne.Do(func() { statusLabel.SetText("Fetching…") })

		// jobs list
		raw := runRemoteCommandToString(cfg, h, p, []string{"schedule", "list"})
		newJobRows := parseScheduleList(raw)
		jobsMu.Lock()
		jobRows = newJobRows
		jobsMu.Unlock()

		// history
		rawHist := runRemoteCommandToString(cfg, h, p, []string{"schedule", "history"})
		newHist := parseHistoryList(rawHist)
		histMu.Lock()
		histRows = newHist
		histMu.Unlock()

		fyne.Do(func() {
			jobsTable.Refresh()
			histTable.Refresh()
			statusLabel.SetText(fmt.Sprintf("%d jobs, %d history entries", len(newJobRows), len(newHist)))
		})
	}

	// ── add job form ──
	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder("job name (no spaces)")

	cronEntry := widget.NewEntry()
	cronEntry.SetPlaceHolder("@daily  or  0 2 * * *")

	cmdSelect := widget.NewSelect([]string{"scan"}, nil)
	cmdSelect.SetSelected("scan")

	addBtn := widget.NewButton("Add Job", func() {
		name := strings.TrimSpace(nameEntry.Text)
		cronExpr := strings.TrimSpace(cronEntry.Text)
		cmd := cmdSelect.Selected
		if name == "" || cronExpr == "" {
			setText(output, "Name and schedule are required.")
			return
		}
		cfg, h, p := addr()
		payload := name + "|" + cronExpr + "|" + cmd
		go func() {
			result := runRemoteCommandToString(cfg, h, p, []string{"schedule", "add", payload})
			fyne.Do(func() { setText(output, strings.TrimSpace(result)) })
			refreshFrom(cfg, h, p)
		}()
	})
	addBtn.Importance = widget.HighImportance

	removeBtn := widget.NewButton("Remove Selected", func() {
		if selectedJobName == "" {
			setText(output, "Select a job from the table first.")
			return
		}
		cfg, h, p := addr()
		name := selectedJobName
		go func() {
			result := runRemoteCommandToString(cfg, h, p, []string{"schedule", "remove", name})
			fyne.Do(func() {
				setText(output, strings.TrimSpace(result))
				selectedJobName = ""
			})
			refreshFrom(cfg, h, p)
		}()
	})
	removeBtn.Importance = widget.DangerImportance

	refreshBtn := widget.NewButton("⟳  Refresh", func() {
		cfg, h, p := addr()
		go refreshFrom(cfg, h, p)
	})

	clearBtn := widget.NewButton("Clear", func() { setText(output, "") })

	// ── layout ──
	topBar := container.NewHBox(
		widget.NewLabel("Agent:"), hostEntry,
		widget.NewLabel("Port:"), portEntry,
		widget.NewLabel("User:"), userEntry,
		widget.NewLabel("Pass:"), passEntry,
		refreshBtn,
		widget.NewSeparator(), statusLabel,
	)

	addForm := container.NewHBox(
		widget.NewLabel("Name:"), nameEntry,
		widget.NewLabel("Cron:"), cronEntry,
		widget.NewLabel("Cmd:"), cmdSelect,
		addBtn, removeBtn,
		widget.NewSeparator(), clearBtn,
	)

	jobsPanel := container.NewBorder(
		widget.NewLabelWithStyle("Scheduled Jobs", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		nil, nil, nil, jobsTable,
	)

	histPanel := container.NewBorder(
		widget.NewLabelWithStyle("Run History (newest first)", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		nil, nil, nil, histTable,
	)

	tablesSplit := container.NewHSplit(jobsPanel, histPanel)
	tablesSplit.SetOffset(0.5)

	outputPanel := container.NewBorder(
		widget.NewLabelWithStyle("Output", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		nil, nil, nil, container.NewScroll(output),
	)

	mainSplit := container.NewVSplit(tablesSplit, outputPanel)
	mainSplit.SetOffset(0.65)

	return container.NewBorder(
		container.NewVBox(topBar, addForm),
		nil, nil, nil, mainSplit,
	)
}

// ── Tab 6: Users ──────────────────────────────────────────────────────────────

// makeUsersTab manages the agent users file, either the local file directly or
// a remote agent's file through its (authenticated) "user" command.
func makeUsersTab(config configInfo) fyne.CanvasObject {
	const targetLocal, targetRemote = "Local users file", "Remote agent"
	target := widget.NewRadioGroup([]string{targetLocal, targetRemote}, nil)
	target.Horizontal = true
	target.SetSelected(targetLocal)

	usersDBEntry := widget.NewEntry()
	usersDBEntry.SetText(config.usersDB)
	usersDBEntry.SetPlaceHolder("users.db path")

	hostEntry := widget.NewEntry()
	hostEntry.SetText(config.agentHost)
	hostEntry.SetPlaceHolder("agent host")
	portEntry := widget.NewEntry()
	portEntry.SetText(config.agentPort)
	portEntry.SetPlaceHolder("port")
	userEntry, passEntry := credEntries(config)

	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder("user name")
	newPassEntry := widget.NewPasswordEntry()
	newPassEntry.SetPlaceHolder("password (min 8 chars)")
	roleSelect := widget.NewSelect([]string{roleRO, roleRW, roleAdmin}, nil)
	roleSelect.SetSelected(roleRO)

	statusLabel := widget.NewLabel("Ready")
	output := roEntry()

	run := func(args []string) {
		local := target.Selected == targetLocal
		cfg := config
		cfg.usersDB = usersDBEntry.Text
		cfg.agentUser = userEntry.Text
		cfg.agentPassword = passEntry.Text
		h, p := hostEntry.Text, portEntry.Text
		statusLabel.SetText("Working…")
		go func() {
			var out string
			if local {
				out = userCommand(cfg.usersDB, args)
			} else {
				out = runRemoteCommandToString(cfg, h, p, append([]string{"user"}, args...))
			}
			fyne.Do(func() {
				setText(output, out)
				statusLabel.SetText("Done")
			})
		}()
	}

	needName := func() (string, bool) {
		name := strings.TrimSpace(nameEntry.Text)
		if name == "" {
			setText(output, "Enter a user name first.")
			return "", false
		}
		return name, true
	}

	listBtn := widget.NewButton("List Users", func() { run([]string{"list"}) })
	addBtn := widget.NewButton("Add", func() {
		if name, ok := needName(); ok {
			run([]string{"add", name, newPassEntry.Text, roleSelect.Selected})
		}
	})
	addBtn.Importance = widget.HighImportance
	roleBtn := widget.NewButton("Set Role", func() {
		if name, ok := needName(); ok {
			run([]string{"role", name, roleSelect.Selected})
		}
	})
	passwdBtn := widget.NewButton("Set Password", func() {
		if name, ok := needName(); ok {
			run([]string{"passwd", name, newPassEntry.Text})
		}
	})
	removeBtn := widget.NewButton("Remove", func() {
		if name, ok := needName(); ok {
			run([]string{"remove", name})
		}
	})
	removeBtn.Importance = widget.DangerImportance
	clearBtn := widget.NewButton("Clear", func() { setText(output, "") })

	info := widget.NewLabel(
		"Local: edits the users file on this machine (used by an agent or web GUI started here).\n" +
			"Remote: connects to an agent with the credentials on the right (admin role needed) and manages its users file.\n" +
			"Roles: ro = view/compare reports, rw = ro + scans and schedules, admin = rw + manage users.\n" +
			"A fresh install has the default user \"" + defaultAdminUser + "\" / \"" + defaultAdminPassword + "\" - change it first.",
	)

	targetBar := container.NewHBox(
		target,
		widget.NewSeparator(),
		widget.NewLabel("Users file:"), usersDBEntry,
		widget.NewSeparator(),
		widget.NewLabel("Agent:"), hostEntry,
		widget.NewLabel("Port:"), portEntry,
		widget.NewLabel("User:"), userEntry,
		widget.NewLabel("Pass:"), passEntry,
	)
	editBar := container.NewHBox(
		widget.NewLabel("Name:"), nameEntry,
		widget.NewLabel("New password:"), newPassEntry,
		widget.NewLabel("Role:"), roleSelect,
		listBtn, addBtn, passwdBtn, roleBtn, removeBtn,
		widget.NewSeparator(), clearBtn,
		widget.NewSeparator(), statusLabel,
	)

	return container.NewBorder(
		container.NewVBox(info, targetBar, editBar),
		nil, nil, nil,
		container.NewScroll(output),
	)
}

type schedJobRow struct{ name, schedule, command, next string }
type histJobRow struct{ name, start, end, status string }

// The agent's "schedule list" / "schedule history" output is a padded text
// table.  Cron expressions ("0 2 * * *") and timestamps ("2025-09-26 02:00:00")
// contain spaces, so the rows cannot be split on whitespace; instead match
// each line by the shape of its columns.
var (
	tsPat        = `\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}`
	scheduleLine = regexp.MustCompile(`^(\S+)\s+(.+?)\s+(\S+)\s+(` + tsPat + `|-)\s*$`)
	historyLine  = regexp.MustCompile(`^(\S+)\s+(` + tsPat + `)\s+(` + tsPat + `|-)\s+(.*?)\s*$`)
)

// parseScheduleList converts the text output of "schedule list" into table rows.
func parseScheduleList(raw string) []schedJobRow {
	var out []schedJobRow
	for _, line := range strings.Split(raw, "\n") {
		m := scheduleLine.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil || m[1] == "NAME" {
			continue // header, separator, blank or malformed line
		}
		out = append(out, schedJobRow{name: m[1], schedule: m[2], command: m[3], next: m[4]})
	}
	return out
}

// parseHistoryList converts the text output of "schedule history" into table rows.
func parseHistoryList(raw string) []histJobRow {
	var out []histJobRow
	for _, line := range strings.Split(raw, "\n") {
		m := historyLine.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		out = append(out, histJobRow{name: m[1], start: m[2], end: m[3], status: m[4]})
	}
	return out
}
