package main

import (
	"fmt"
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
		container.NewTabItem("Agent",          makeAgentTab(config)),
		container.NewTabItem("Remote",         makeRemoteTab(config)),
		container.NewTabItem("Hosts",          makeHostsTab(config)),
		container.NewTabItem("Schedule",       makeScheduleTab(config)),
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

	statusLabel    := widget.NewLabel("Ready")
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
		cfg.path       = pathEntry.Text
		cfg.reportName = reportNameEntry.Text
		cfg.reportDir  = reportDirEntry.Text
		return cfg
	}

	refreshList := func() {
		reports = parseReportList(listReports(currentConfig()))
		reportList.Refresh()
	}

	// ── action buttons ──
	scanBtn := widget.NewButton("▶  Scan", func() {
		statusLabel.SetText("Scanning…")
		cfg := currentConfig()
		go func() {
			callScan(cfg)
			refreshList()
			statusLabel.SetText("Scan complete")
		}()
	})
	scanBtn.Importance = widget.HighImportance

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
		go func() {
			result := compareReportsString(cfg, compareOld, compareNew)
			setText(output, result)
			statusLabel.SetText("Compare complete — result saved to reportDir")
		}()
	})
	compareBtn.Importance = widget.WarningImportance

	clearBtn := widget.NewButton("Clear", func() { setText(output, "") })

	// ── layout ──
	configPanel := container.NewVBox(
		container.NewGridWithColumns(2,
			widget.NewLabel("Scan Path:"),    pathEntry,
			widget.NewLabel("Report Name:"),  reportNameEntry,
			widget.NewLabel("Report Dir:"),   reportDirEntry,
		),
		container.NewHBox(scanBtn, refreshBtn, widget.NewSeparator(), statusLabel),
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

	go refreshList() // populate on load

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
	logOutput   := roEntry()

	var (
		mu     sync.Mutex
		stopCh chan struct{}
	)

	startBtn := widget.NewButton("▶  Start Agent", nil)
	stopBtn  := widget.NewButton("■  Stop Agent",  nil)
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
		cfg.agentPort  = agentPortEntry.Text
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
			statusLabel.SetText("Agent: stopped")
			startBtn.Enable()
			stopBtn.Disable()
			agentHostEntry.Enable()
			agentPortEntry.Enable()
			appendText(logOutput, "Agent stopped\n")
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

	statusLabel     := widget.NewLabel("Idle")
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

	addr := func() (string, string) { return hostEntry.Text, portEntry.Text }

	statusBtn := widget.NewButton("Status", func() {
		h, p := addr()
		go func() {
			statusLabel.SetText("Checking…")
			result := runRemoteCommandToString(h, p, []string{"status"})
			statusLabel.SetText(strings.TrimSpace(result))
		}()
	})

	listBtn := widget.NewButton("⟳  List Reports", func() {
		h, p := addr()
		statusLabel.SetText("Listing…")
		go func() {
			raw := runRemoteCommandToString(h, p, []string{"list"})
			remoteReports = parseReportList(raw)
			remoteList.Refresh()
			statusLabel.SetText(fmt.Sprintf("%d reports", len(remoteReports)))
		}()
	})

	scanBtn := widget.NewButton("▶  Scan", func() {
		h, p := addr()
		statusLabel.SetText("Scan running on remote…")
		go func() {
			result := runRemoteCommandToString(h, p, []string{"scan"})
			setText(output, result)
			statusLabel.SetText("Scan complete")
		}()
	})
	scanBtn.Importance = widget.HighImportance

	viewBtn := widget.NewButton("View Data", func() {
		if selectedRemote == "" {
			setText(output, "Select a report first.")
			return
		}
		h, p := addr()
		go func() { setText(output, runRemoteCommandToString(h, p, []string{"data", selectedRemote})) }()
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
		h, p := addr()
		statusLabel.SetText("Comparing on remote…")
		go func() {
			result := runRemoteCommandToString(h, p, []string{"compare", compareOld, compareNew})
			setText(output, result)
			statusLabel.SetText("Compare complete")
		}()
	})
	compareBtn.Importance = widget.WarningImportance

	clearBtn := widget.NewButton("Clear", func() { setText(output, "") })

	topBar := container.NewHBox(
		widget.NewLabel("Host:"), hostEntry,
		widget.NewLabel("Port:"), portEntry,
		statusBtn, listBtn, scanBtn,
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

	output      := roEntry()
	statusLabel := widget.NewLabel("No hosts loaded")

	var (
		hostsMu sync.Mutex
		hosts   []remoteHost
	)

	// ── hosts table ──
	// Row 0 is the header; data rows start at 1.
	const numCols = 6
	colHeaders := []string{"Alias", "Address", "Port", "Path", "Report Name", "SSH User"}
	colWidths  := []float32{120, 160, 60, 200, 160, 100}

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
			vals := []string{h.alias, h.address, h.port, h.path, h.reportName, h.sshUser}
			lbl.SetText(vals[id.Col])
		},
	)
	for i, w := range colWidths {
		hostsTable.SetColumnWidth(i, w)
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
		statusLabel.SetText("Pinging…")
		go func() {
			results := make([]hostResult, len(snapshot))
			var wg sync.WaitGroup
			for i, h := range snapshot {
				wg.Add(1)
				go func(idx int, host remoteHost) {
					defer wg.Done()
					out := runRemoteCommandToString(host.address, host.port, []string{"status"})
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
			setText(output, sb.String())
			statusLabel.SetText("Ping complete")
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
				appendText(output, fmt.Sprintf("[%s] syncing…\n", h.alias))
				syncHostReports(cfg, h)
			}
			statusLabel.SetText("Sync complete")
			appendText(output, "\nAll hosts synced.\n")
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
		statusLabel.SetText("Running…")
		go func() {
			results := make([]hostResult, len(snapshot))
			var wg sync.WaitGroup
			for i, h := range snapshot {
				wg.Add(1)
				go func(idx int, host remoteHost) {
					defer wg.Done()
					out := runRemoteCommandToString(host.address, host.port, cmdArgs)
					results[idx] = hostResult{alias: host.alias, output: out}
				}(i, h)
			}
			wg.Wait()
			var sb strings.Builder
			for _, r := range results {
				sb.WriteString(fmt.Sprintf("=== %s ===\n%s\n", r.alias, r.output))
			}
			setText(output, sb.String())
			statusLabel.SetText("Done")
		}()
	})

	// ── start agents via SSH ──
	startAllBtn := widget.NewButton("Start All (SSH)", func() {
		hostsMu.Lock()
		snapshot := append([]remoteHost(nil), hosts...)
		hostsMu.Unlock()
		if len(snapshot) == 0 {
			setText(output, "No hosts loaded.")
			return
		}
		cfg := currentCfg()
		statusLabel.SetText("Starting agents…")
		go func() {
			cmdStartAgent(cfg, "") // empty alias = all hosts
			statusLabel.SetText("Start commands sent")
		}()
		_ = cfg
	})

	clearBtn := widget.NewButton("Clear", func() { setText(output, "") })

	// ── layout ──
	topBar := container.NewHBox(
		widget.NewLabel("Hosts Config:"), hostsConfigEntry, loadBtn,
		widget.NewSeparator(), statusLabel,
	)

	actionBar := container.NewHBox(
		pingAllBtn, syncAllBtn, startAllBtn,
		widget.NewSeparator(),
		widget.NewLabel("Cmd:"), cmdEntry, runAllBtn,
		widget.NewSeparator(),
		clearBtn,
	)

	split := container.NewVSplit(
		hostsTable,
		container.NewScroll(output),
	)
	split.SetOffset(0.45)

	go loadHosts() // try to load on startup

	return container.NewBorder(topBar, actionBar, nil, nil, split)
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
	output      := roEntry()

	addr := func() (string, string) { return hostEntry.Text, portEntry.Text }

	// ── scheduled jobs table ──
	const jobCols = 4
	jobHeaders  := []string{"Name", "Schedule", "Command", "Next Run"}
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
	histHeaders   := []string{"Name", "Start", "End", "Status"}
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
	refresh := func() {
		h, p := addr()
		go func() {
			statusLabel.SetText("Fetching…")

			// jobs list
			raw := runRemoteCommandToString(h, p, []string{"schedule", "list"})
			newJobRows := parseScheduleList(raw)
			jobsMu.Lock()
			jobRows = newJobRows
			jobsMu.Unlock()
			jobsTable.Refresh()

			// history
			rawHist := runRemoteCommandToString(h, p, []string{"schedule", "history"})
			newHist := parseHistoryList(rawHist)
			histMu.Lock()
			histRows = newHist
			histMu.Unlock()
			histTable.Refresh()

			statusLabel.SetText(fmt.Sprintf("%d jobs, %d history entries", len(newJobRows), len(newHist)))
		}()
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
		h, p := addr()
		payload := name + "|" + cronExpr + "|" + cmd
		go func() {
			result := runRemoteCommandToString(h, p, []string{"schedule", "add", payload})
			setText(output, strings.TrimSpace(result))
			refresh()
		}()
	})
	addBtn.Importance = widget.HighImportance

	removeBtn := widget.NewButton("Remove Selected", func() {
		if selectedJobName == "" {
			setText(output, "Select a job from the table first.")
			return
		}
		h, p := addr()
		name := selectedJobName
		go func() {
			result := runRemoteCommandToString(h, p, []string{"schedule", "remove", name})
			setText(output, strings.TrimSpace(result))
			selectedJobName = ""
			refresh()
		}()
	})
	removeBtn.Importance = widget.DangerImportance

	refreshBtn := widget.NewButton("⟳  Refresh", func() { refresh() })

	clearBtn := widget.NewButton("Clear", func() { setText(output, "") })

	// ── layout ──
	topBar := container.NewHBox(
		widget.NewLabel("Agent:"), hostEntry,
		widget.NewLabel("Port:"), portEntry,
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

type schedJobRow struct{ name, schedule, command, next string }
type histJobRow  struct{ name, start, end, status string }

// parseScheduleList converts the text output of "schedule list" into table rows.
func parseScheduleList(raw string) []schedJobRow {
	var out []schedJobRow
	for i, line := range strings.Split(raw, "\n") {
		if i < 2 { continue } // skip header + separator
		line = strings.TrimSpace(line)
		if line == "" { continue }
		fields := splitFields(line, 4)
		if len(fields) < 4 { continue }
		out = append(out, schedJobRow{
			name:     fields[0],
			schedule: fields[1],
			command:  fields[2],
			next:     fields[3],
		})
	}
	return out
}

// parseHistoryList converts the text output of "schedule history" into table rows.
func parseHistoryList(raw string) []histJobRow {
	var out []histJobRow
	for i, line := range strings.Split(raw, "\n") {
		if i < 2 { continue }
		line = strings.TrimSpace(line)
		if line == "" { continue }
		fields := splitFields(line, 4)
		if len(fields) < 4 { continue }
		out = append(out, histJobRow{
			name:   fields[0],
			start:  fields[1],
			end:    fields[2],
			status: fields[3],
		})
	}
	return out
}

// splitFields splits a fixed-width text line into at most n fields by
// collapsing runs of spaces as delimiters.
func splitFields(s string, n int) []string {
	parts := strings.Fields(s)
	if len(parts) <= n {
		return parts
	}
	// Merge trailing parts back into the last field.
	return append(parts[:n-1], strings.Join(parts[n-1:], " "))
}
