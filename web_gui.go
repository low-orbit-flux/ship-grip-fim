package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"text/template"
)

// ── web GUI state ─────────────────────────────────────────────────────────────

type webGUISrv struct {
	mu           sync.Mutex
	agentStop    chan struct{}
	agentRunning bool
}

var webSrv = webGUISrv{}

// ── startup ───────────────────────────────────────────────────────────────────

func startWebGUI(config configInfo) {
	mux := http.NewServeMux()

	h := func(fn func(configInfo, http.ResponseWriter, *http.Request)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) { fn(config, w, r) }
	}

	mux.HandleFunc("/", h(webIndex))
	mux.HandleFunc("/api/reports", h(webListReports))
	mux.HandleFunc("/api/report/", h(webGetReport))
	mux.HandleFunc("/api/scan", h(webScan))
	mux.HandleFunc("/api/compare", h(webCompare))
	mux.HandleFunc("/api/quickcompare", h(webQuickCompare))
	mux.HandleFunc("/api/status", h(webStatus))
	mux.HandleFunc("/api/agent/start", h(webAgentStart))
	mux.HandleFunc("/api/agent/stop", func(w http.ResponseWriter, r *http.Request) { webAgentStop(w, r) })
	mux.HandleFunc("/api/remote", func(w http.ResponseWriter, r *http.Request) { webRemote(w, r) })
	mux.HandleFunc("/api/hosts", h(webHosts))
	mux.HandleFunc("/api/hosts/pingall", h(webPingAll))
	mux.HandleFunc("/api/hosts/sync", h(webSyncAll))
	mux.HandleFunc("/api/hosts/start", h(webStartAll))
	mux.HandleFunc("/api/hosts/remoteall", h(webRemoteAll))

	addr := config.webHost + ":" + config.webPort
	fmt.Printf("Web GUI listening on http://%s\n", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		fmt.Println("ERROR - web GUI:", err)
	}
}

// ── tiny helpers ──────────────────────────────────────────────────────────────

func wJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

func wErr(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg}) //nolint:errcheck
}

func wDecode(r *http.Request, v interface{}) error {
	return json.NewDecoder(r.Body).Decode(v)
}

func requirePost(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		wErr(w, "POST required", 405)
		return false
	}
	return true
}

// ── handlers ──────────────────────────────────────────────────────────────────

func webIndex(config configInfo, w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	tmpl := template.Must(template.New("p").Parse(webHTML))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tmpl.Execute(w, map[string]string{ //nolint:errcheck
		"Path":       config.path,
		"ReportName": config.reportName,
		"ReportDir":  config.reportDir,
		"AgentHost":  config.agentHost,
		"AgentPort":  config.agentPort,
	})
}

func webListReports(config configInfo, w http.ResponseWriter, r *http.Request) {
	raw := listReports(config)
	var out []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "compare__") ||
			strings.ContainsAny(line, " \t") ||
			strings.HasPrefix(line, "-") || strings.HasPrefix(line, "=") {
			continue
		}
		out = append(out, line)
	}
	wJSON(w, map[string]interface{}{"reports": out})
}

func webGetReport(config configInfo, w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/report/")
	if id == "" {
		wErr(w, "missing report ID", 400)
		return
	}
	wJSON(w, map[string]string{"output": listReportDataString(config, id)})
}

func webScan(config configInfo, w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	var req struct {
		Path       string `json:"path"`
		ReportName string `json:"reportName"`
		ReportDir  string `json:"reportDir"`
	}
	wDecode(r, &req) //nolint:errcheck

	c := config
	if req.Path != "" {
		c.path = req.Path
	}
	if req.ReportName != "" {
		c.reportName = req.ReportName
	}
	if req.ReportDir != "" {
		c.reportDir = req.ReportDir
	}

	state.mu.Lock()
	if state.scanRunning || state.compareRunning {
		state.mu.Unlock()
		wErr(w, "an operation is already running", 409)
		return
	}
	state.scanRunning = true
	state.mu.Unlock()

	go func() {
		callScan(c)
		state.mu.Lock()
		state.scanRunning = false
		state.mu.Unlock()
	}()

	wJSON(w, map[string]bool{"started": true})
}

func webCompare(config configInfo, w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	var req struct {
		Older     string `json:"older"`
		Newer     string `json:"newer"`
		ReportDir string `json:"reportDir"`
	}
	wDecode(r, &req) //nolint:errcheck
	if req.Older == "" || req.Newer == "" {
		wErr(w, "older and newer required", 400)
		return
	}
	c := config
	if req.ReportDir != "" {
		c.reportDir = req.ReportDir
	}
	wJSON(w, map[string]string{"output": compareReportsString(c, req.Older, req.Newer)})
}

func webQuickCompare(config configInfo, w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	var req struct {
		ReportName string `json:"reportName"`
		ReportDir  string `json:"reportDir"`
	}
	wDecode(r, &req) //nolint:errcheck
	c := config
	if req.ReportName != "" {
		c.reportName = req.ReportName
	}
	if req.ReportDir != "" {
		c.reportDir = req.ReportDir
	}
	wJSON(w, map[string]string{"output": quickCompareString(c)})
}

func webStatus(config configInfo, w http.ResponseWriter, r *http.Request) {
	state.mu.Lock()
	sr := state.scanRunning
	cr := state.compareRunning
	var lsCopy *scanStats
	if state.lastScan != nil {
		cp := *state.lastScan
		lsCopy = &cp
	}
	state.mu.Unlock()

	webSrv.mu.Lock()
	ar := webSrv.agentRunning
	webSrv.mu.Unlock()

	resp := map[string]interface{}{
		"scanRunning":    sr,
		"compareRunning": cr,
		"agentRunning":   ar,
	}
	if lsCopy != nil {
		resp["lastScan"] = map[string]interface{}{
			"reportName": lsCopy.ReportName,
			"fileCount":  lsCopy.FileCount,
			"duration":   lsCopy.Duration.Seconds(),
			"timestamp":  lsCopy.Timestamp.Format("2006-01-02 15:04:05"),
		}
	}
	wJSON(w, resp)
}

func webAgentStart(config configInfo, w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	var req struct {
		Host string `json:"host"`
		Port string `json:"port"`
	}
	wDecode(r, &req) //nolint:errcheck

	webSrv.mu.Lock()
	defer webSrv.mu.Unlock()
	if webSrv.agentRunning {
		wErr(w, "agent already running", 409)
		return
	}
	c := config
	if req.Host != "" {
		c.agentHost = req.Host
	}
	if req.Port != "" {
		c.agentPort = req.Port
	}
	stop := make(chan struct{})
	webSrv.agentStop = stop
	webSrv.agentRunning = true
	go func() {
		startAgentServerWithStop(c, stop)
		webSrv.mu.Lock()
		webSrv.agentRunning = false
		webSrv.agentStop = nil
		webSrv.mu.Unlock()
	}()
	wJSON(w, map[string]string{"msg": "Agent started on " + c.agentHost + ":" + c.agentPort})
}

func webAgentStop(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	webSrv.mu.Lock()
	defer webSrv.mu.Unlock()
	if !webSrv.agentRunning || webSrv.agentStop == nil {
		wErr(w, "agent not running", 409)
		return
	}
	close(webSrv.agentStop)
	wJSON(w, map[string]string{"msg": "Agent stop signal sent"})
}

func webRemote(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	var req struct {
		Host string   `json:"host"`
		Port string   `json:"port"`
		Cmd  []string `json:"cmd"`
	}
	if err := wDecode(r, &req); err != nil || req.Host == "" || req.Port == "" || len(req.Cmd) == 0 {
		wErr(w, "host, port, and cmd required", 400)
		return
	}
	wJSON(w, map[string]string{"output": runRemoteCommandToString(req.Host, req.Port, req.Cmd)})
}

func webHosts(config configInfo, w http.ResponseWriter, r *http.Request) {
	hosts, err := parseHostsConfig(config.hostsConfig)
	if err != nil {
		wErr(w, err.Error(), 500)
		return
	}
	type hj struct {
		Alias      string `json:"alias"`
		Address    string `json:"address"`
		Port       string `json:"port"`
		Path       string `json:"path"`
		ReportName string `json:"reportName"`
		SSHUser    string `json:"sshUser"`
	}
	out := make([]hj, len(hosts))
	for i, h := range hosts {
		out[i] = hj{h.alias, h.address, h.port, h.path, h.reportName, h.sshUser}
	}
	wJSON(w, map[string]interface{}{"hosts": out})
}

func webPingAll(config configInfo, w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	hosts, err := parseHostsConfig(config.hostsConfig)
	if err != nil {
		wErr(w, err.Error(), 500)
		return
	}
	res := make([]hostResult, len(hosts))
	var wg sync.WaitGroup
	for i, h := range hosts {
		wg.Add(1)
		go func(idx int, host remoteHost) {
			defer wg.Done()
			out := runRemoteCommandToString(host.address, host.port, []string{"status"})
			res[idx] = hostResult{alias: host.alias, output: strings.TrimSpace(out)}
		}(i, h)
	}
	wg.Wait()
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%-18s %s\n", "ALIAS", "STATUS"))
	sb.WriteString(strings.Repeat("-", 45) + "\n")
	for _, rr := range res {
		sb.WriteString(fmt.Sprintf("%-18s %s\n", rr.alias, rr.output))
	}
	wJSON(w, map[string]string{"output": sb.String()})
}

func webSyncAll(config configInfo, w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	hosts, err := parseHostsConfig(config.hostsConfig)
	if err != nil {
		wErr(w, err.Error(), 500)
		return
	}
	var sb strings.Builder
	for _, h := range hosts {
		sb.WriteString(fmt.Sprintf("[%s] syncing...\n", h.alias))
		syncHostReports(config, h)
		sb.WriteString(fmt.Sprintf("[%s] done\n", h.alias))
	}
	wJSON(w, map[string]string{"output": sb.String()})
}

func webStartAll(config configInfo, w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	go cmdStartAgent(config, "")
	wJSON(w, map[string]string{"msg": "Start commands sent to all hosts (check server log for SSH output)"})
}

func webRemoteAll(config configInfo, w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	var req struct {
		Cmd []string `json:"cmd"`
	}
	wDecode(r, &req) //nolint:errcheck
	if len(req.Cmd) == 0 {
		wErr(w, "cmd required", 400)
		return
	}
	hosts, err := parseHostsConfig(config.hostsConfig)
	if err != nil {
		wErr(w, err.Error(), 500)
		return
	}
	res := make([]hostResult, len(hosts))
	var wg sync.WaitGroup
	for i, h := range hosts {
		wg.Add(1)
		go func(idx int, host remoteHost) {
			defer wg.Done()
			res[idx] = hostResult{alias: host.alias, output: runRemoteCommandToString(host.address, host.port, req.Cmd)}
		}(i, h)
	}
	wg.Wait()
	var sb strings.Builder
	for _, rr := range res {
		sb.WriteString(fmt.Sprintf("=== %s ===\n%s\n", rr.alias, rr.output))
	}
	wJSON(w, map[string]string{"output": sb.String()})
}

// ── embedded single-page HTML app ─────────────────────────────────────────────

var webHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>ship-grip-fim</title>
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:'Courier New',monospace;font-size:13px;background:#0d1117;color:#c9d1d9;display:flex;flex-direction:column;height:100vh;overflow:hidden}
header{background:#161b22;border-bottom:1px solid #30363d;padding:9px 18px;display:flex;align-items:center;gap:12px;flex-shrink:0}
header h1{color:#4ade80;font-size:1.05em;letter-spacing:1px}
header .sub{color:#8b949e;font-size:.85em}
.tabs{display:flex;background:#161b22;border-bottom:1px solid #30363d;padding:0 12px;flex-shrink:0}
.tb{padding:9px 16px;border:none;border-bottom:2px solid transparent;background:none;color:#8b949e;cursor:pointer;font-family:inherit;font-size:12px}
.tb:hover{color:#c9d1d9}
.tb.on{color:#4ade80;border-bottom-color:#4ade80}
.pnl{display:none;flex-direction:column;gap:10px;padding:14px 18px;flex:1;overflow-y:auto}
.pnl.on{display:flex}
.card{background:#161b22;border:1px solid #30363d;border-radius:5px;padding:12px}
.ct{color:#4ade80;font-size:.75em;letter-spacing:1px;text-transform:uppercase;font-weight:bold;margin-bottom:8px}
.row{display:flex;align-items:center;gap:7px;flex-wrap:wrap}
.mt{margin-top:8px}
label{color:#8b949e;font-size:.85em;white-space:nowrap}
.inp{background:#0d1117;border:1px solid #30363d;border-radius:3px;color:#c9d1d9;padding:4px 8px;font-family:inherit;font-size:12px;outline:none}
.inp:focus{border-color:#4ade80}
.inp-sm{width:110px}
.inp-md{width:200px}
.inp-lg{flex:1;min-width:120px}
.btn{display:inline-flex;align-items:center;gap:4px;padding:5px 12px;border:1px solid;border-radius:3px;font-family:inherit;font-size:12px;cursor:pointer;background:transparent}
.btn:disabled{opacity:.4;cursor:not-allowed;pointer-events:none}
.g{border-color:#4ade80;color:#4ade80}.g:hover{background:#4ade80;color:#0d1117}
.s{border-color:#30363d;color:#8b949e}.s:hover{border-color:#8b949e;color:#c9d1d9}
.y{border-color:#f59e0b;color:#f59e0b}.y:hover{background:#f59e0b;color:#0d1117}
.r{border-color:#ef4444;color:#ef4444}.r:hover{background:#ef4444;color:#fff}
.sep{width:1px;height:18px;background:#30363d}
.bdg{padding:2px 8px;border-radius:9px;font-size:.75em;font-weight:bold}
.bi{background:#1a2d1a;color:#4ade80}
.br{background:#2d1a1a;color:#ef4444}
.bs{background:#1a1a2d;color:#8b949e}
.split{display:flex;gap:10px;flex:1;overflow:hidden;min-height:0}
.sl{width:280px;flex-shrink:0;display:flex;flex-direction:column;overflow:hidden}
.sr{flex:1;display:flex;flex-direction:column;overflow:hidden;gap:8px}
.tw{overflow:auto;flex:1;border:1px solid #30363d;border-radius:3px;min-height:40px}
table{width:100%;border-collapse:collapse}
th{background:#0d1117;color:#4ade80;padding:5px 9px;text-align:left;font-size:.75em;letter-spacing:.5px;border-bottom:1px solid #30363d;position:sticky;top:0}
td{padding:5px 9px;border-bottom:1px solid #21262d;font-size:.8em;overflow:hidden;text-overflow:ellipsis;max-width:240px}
tr:hover td{background:#1c2128;cursor:pointer}
tr.sel td{background:#1a2a1a;color:#4ade80}
.out{background:#000;border:1px solid #21262d;border-radius:3px;padding:10px;overflow:auto;white-space:pre;font-size:.78em;color:#4ade80;flex:1;min-height:60px}
</style>
</head>
<body>
<header>
  <h1>&#x1F512; ship-grip-fim</h1>
  <span class="sub">File Integrity Monitor</span>
  <span id="gst" class="bdg bi">idle</span>
</header>
<div class="tabs">
  <button class="tb on"  onclick="tab('local',this)">Local</button>
  <button class="tb"     onclick="tab('agent',this)">Agent</button>
  <button class="tb"     onclick="tab('remote',this)">Remote</button>
  <button class="tb"     onclick="tab('hosts',this)">Hosts</button>
  <button class="tb"     onclick="tab('sched',this)">Schedule</button>
</div>

<!-- ── LOCAL ─────────────────────────────────────────────────────── -->
<div id="local" class="pnl on">
  <div class="card">
    <div class="row">
      <label>Path</label><input id="lpath" class="inp inp-lg" value="{{index . "Path"}}">
      <label>Report&nbsp;Name</label><input id="lname" class="inp inp-md" value="{{index . "ReportName"}}">
      <label>Report&nbsp;Dir</label><input id="ldir"  class="inp inp-md" value="{{index . "ReportDir"}}">
    </div>
    <div class="row mt">
      <button class="btn g" id="lscanbtn" onclick="lScan()">&#x25B6; Scan</button>
      <button class="btn y" onclick="lQC()">&#x26A1; Quick Compare</button>
      <button class="btn s" onclick="lLoad()">&#x27F3; Refresh</button>
      <span class="sep"></span>
      <span id="lst" class="bdg bi">idle</span>
    </div>
  </div>
  <div class="split">
    <div class="sl card" style="padding:8px;gap:6px">
      <div class="ct">Reports</div>
      <div class="tw"><table><thead><tr><th>Report ID</th></tr></thead><tbody id="lrpts"></tbody></table></div>
      <div class="row" style="gap:4px">
        <button class="btn s" onclick="lView()">View</button>
        <button class="btn s" onclick="lSOld()">Set Old</button>
        <button class="btn s" onclick="lSNew()">Set New</button>
      </div>
    </div>
    <div class="sr">
      <div class="card" style="padding:8px">
        <div class="row">
          <label>Old</label><input id="lold" class="inp inp-lg" placeholder="old report ID">
          <label>New</label><input id="lnew" class="inp inp-lg" placeholder="new report ID">
          <button class="btn y" onclick="lCmp()">Compare</button>
          <button class="btn s" onclick="clr('lout')">Clear</button>
        </div>
      </div>
      <pre class="out" id="lout">(output will appear here)</pre>
    </div>
  </div>
</div>

<!-- ── AGENT ─────────────────────────────────────────────────────── -->
<div id="agent" class="pnl">
  <div class="card">
    <div class="row">
      <label>Bind</label><input id="ahost" class="inp inp-sm" value="{{index . "AgentHost"}}">
      <label>Port</label><input id="aport" class="inp" style="width:65px" value="{{index . "AgentPort"}}">
      <button class="btn g" id="abtn" onclick="aStart()">&#x25B6; Start Agent</button>
      <button class="btn r" id="astop" onclick="aStop()" disabled>&#x25A0; Stop</button>
      <span class="sep"></span>
      <span id="ast" class="bdg bs">stopped</span>
    </div>
    <p style="margin-top:8px;color:#8b949e;font-size:.85em">
      Set Bind to 0.0.0.0 to accept remote connections.
      The agent accepts TCP connections from remote / sync / remoteall commands.
    </p>
  </div>
  <pre class="out" id="aout" style="flex:1">(agent log)</pre>
</div>

<!-- ── REMOTE ────────────────────────────────────────────────────── -->
<div id="remote" class="pnl">
  <div class="card">
    <div class="row">
      <label>Host</label><input id="rhost" class="inp inp-sm" value="localhost">
      <label>Port</label><input id="rport" class="inp" style="width:65px" value="{{index . "AgentPort"}}">
      <button class="btn s" onclick="rStat()">Status</button>
      <button class="btn s" onclick="rList()">&#x27F3; List</button>
      <button class="btn g" onclick="rScan()">&#x25B6; Scan</button>
      <button class="btn y" onclick="rQC()">&#x26A1; Quick Compare</button>
      <span class="sep"></span>
      <span id="rst" class="bdg bi">—</span>
    </div>
  </div>
  <div class="split">
    <div class="sl card" style="padding:8px;gap:6px">
      <div class="ct">Remote Reports</div>
      <div class="tw"><table><thead><tr><th>Report ID</th></tr></thead><tbody id="rrpts"></tbody></table></div>
      <div class="row" style="gap:4px">
        <button class="btn s" onclick="rView()">View</button>
        <button class="btn s" onclick="rSOld()">Set Old</button>
        <button class="btn s" onclick="rSNew()">Set New</button>
      </div>
    </div>
    <div class="sr">
      <div class="card" style="padding:8px">
        <div class="row">
          <label>Old</label><input id="rold" class="inp inp-lg" placeholder="old report ID">
          <label>New</label><input id="rnew" class="inp inp-lg" placeholder="new report ID">
          <button class="btn y" onclick="rCmp()">Compare</button>
          <button class="btn s" onclick="clr('rout')">Clear</button>
        </div>
      </div>
      <pre class="out" id="rout"></pre>
    </div>
  </div>
</div>

<!-- ── HOSTS ─────────────────────────────────────────────────────── -->
<div id="hosts" class="pnl">
  <div class="card">
    <div class="row">
      <button class="btn s" onclick="hLoad()">&#x27F3; Refresh Hosts</button>
      <button class="btn s" onclick="hPing()">Ping All</button>
      <button class="btn s" onclick="hSync()">Sync Reports</button>
      <button class="btn s" onclick="hStart()">Start All (SSH)</button>
      <span class="sep"></span>
      <input id="hcmd" class="inp inp-md" placeholder="command  e.g. scan, list, status">
      <button class="btn g" onclick="hAll()">Run on All</button>
      <span class="sep"></span>
      <span id="hst" class="bdg bi">—</span>
    </div>
  </div>
  <div class="card tw" style="flex:none;max-height:200px">
    <table>
      <thead><tr><th>Alias</th><th>Address</th><th>Port</th><th>Path</th><th>Report Name</th><th>SSH User</th></tr></thead>
      <tbody id="hhosts"></tbody>
    </table>
  </div>
  <pre class="out" id="hout" style="flex:1"></pre>
</div>

<!-- ── SCHEDULE ──────────────────────────────────────────────────── -->
<div id="sched" class="pnl">
  <div class="card">
    <div class="row">
      <label>Agent</label><input id="shost" class="inp inp-sm" value="localhost">
      <label>Port</label><input id="sport" class="inp" style="width:65px" value="{{index . "AgentPort"}}">
      <button class="btn s" onclick="sLoad()">&#x27F3; Connect / Refresh</button>
      <span class="sep"></span>
      <span id="sst" class="bdg bi">—</span>
    </div>
  </div>
  <div class="split">
    <div class="sl" style="gap:8px">
      <div class="card" style="padding:8px;flex:1;display:flex;flex-direction:column;gap:6px">
        <div class="ct">Scheduled Jobs</div>
        <div class="tw">
          <table><thead><tr><th>Name</th><th>Schedule</th><th>Cmd</th><th>Next Run</th></tr></thead>
          <tbody id="sjobs"></tbody></table>
        </div>
        <div class="row mt" style="gap:4px">
          <input id="sjn" class="inp" style="width:90px" placeholder="name">
          <input id="sjc" class="inp" style="width:105px" placeholder="@daily / 0 2 * * *">
          <input id="sjcmd" class="inp" style="width:55px" value="scan">
          <button class="btn g" onclick="sAdd()">Add</button>
          <button class="btn r" onclick="sDel()">Remove</button>
        </div>
      </div>
    </div>
    <div class="sr">
      <div class="card" style="padding:8px;flex:1;display:flex;flex-direction:column;gap:6px">
        <div class="ct">Run History (newest first)</div>
        <div class="tw">
          <table><thead><tr><th>Name</th><th>Start</th><th>End</th><th>Status</th></tr></thead>
          <tbody id="shist"></tbody></table>
        </div>
      </div>
      <pre class="out" id="sout" style="flex:none;height:90px"></pre>
    </div>
  </div>
</div>

<script>
// ── tab switching ─────────────────────────────────────────────────────────
function tab(id,b){
  document.querySelectorAll('.pnl').forEach(p=>p.classList.remove('on'));
  document.querySelectorAll('.tb').forEach(x=>x.classList.remove('on'));
  document.getElementById(id).classList.add('on');
  b.classList.add('on');
}

// ── api ───────────────────────────────────────────────────────────────────
async function api(m,p,b){
  try{
    const o={method:m,headers:{'Content-Type':'application/json'}};
    if(b!==undefined)o.body=JSON.stringify(b);
    const r=await fetch(p,o);
    const t=await r.text();
    try{return JSON.parse(t);}catch{return{error:t};}
  }catch(e){return{error:e.message};}
}

// ── output ────────────────────────────────────────────────────────────────
function so(id,t){const e=document.getElementById(id);if(e){e.textContent=t||'';e.scrollTop=e.scrollHeight;}}
function ao(id,t){const e=document.getElementById(id);if(e){e.textContent+=t;e.scrollTop=e.scrollHeight;}}
function clr(id){so(id,'');}

// ── badge ─────────────────────────────────────────────────────────────────
function bdg(id,txt,cls){const e=document.getElementById(id);if(e){e.textContent=txt;e.className='bdg '+cls;}}

// ── report table builder ──────────────────────────────────────────────────
function buildTbl(tbodyId, rows, selCb, dblCb){
  const tb=document.getElementById(tbodyId);
  tb.innerHTML='';
  (rows||[]).forEach(r=>{
    const tr=document.createElement('tr');
    const td=document.createElement('td');
    td.textContent=r;
    tr.appendChild(td);
    tr.onclick=()=>{tb.querySelectorAll('tr').forEach(x=>x.classList.remove('sel'));tr.classList.add('sel');selCb&&selCb(r);};
    if(dblCb)tr.ondblclick=()=>dblCb(r);
    tb.appendChild(tr);
  });
}

// ── status polling ────────────────────────────────────────────────────────
let _poll=null;
function startPoll(doneCb){
  if(_poll)clearInterval(_poll);
  _poll=setInterval(async()=>{
    const s=await api('GET','/api/status');
    if(!s.scanRunning&&!s.compareRunning){
      clearInterval(_poll);_poll=null;
      bdg('gst','idle','bi');
      if(doneCb)doneCb(s);
    }
  },900);
}

// ── filter raw list output to report IDs ─────────────────────────────────
function parseRpts(raw){
  return (raw||'').split('\n').map(l=>l.trim()).filter(l=>
    l&&!l.includes(' ')&&!l.startsWith('-')&&!l.startsWith('=')&&
    !l.startsWith('ERROR')&&!l.startsWith('No ')&&!l.startsWith('Empty')
  );
}

// ═══════════════════════════════════════════════════════════════════
// LOCAL TAB
// ═══════════════════════════════════════════════════════════════════
let lSel='';

async function lLoad(){
  const d=await api('GET','/api/reports');
  if(d.error){ao('lout','ERROR: '+d.error+'\n');return;}
  buildTbl('lrpts',d.reports,r=>{lSel=r;},r=>{lDoView(r);});
}
function lView(){if(lSel)lDoView(lSel);}
async function lDoView(id){
  const d=await api('GET','/api/report/'+encodeURIComponent(id));
  so('lout',d.output||d.error||'');
}
function lSOld(){if(lSel)document.getElementById('lold').value=lSel;}
function lSNew(){if(lSel)document.getElementById('lnew').value=lSel;}

async function lScan(){
  const path=document.getElementById('lpath').value;
  const name=document.getElementById('lname').value;
  const dir =document.getElementById('ldir').value;
  so('lout','Scan started...\n');
  bdg('lst','scanning','br');bdg('gst','scanning','br');
  document.getElementById('lscanbtn').disabled=true;
  const d=await api('POST','/api/scan',{path,reportName:name,reportDir:dir});
  if(d.error){
    so('lout','ERROR: '+d.error);
    bdg('lst','idle','bi');bdg('gst','idle','bi');
    document.getElementById('lscanbtn').disabled=false;
    return;
  }
  startPoll(s=>{
    const info=s.lastScan?' — '+s.lastScan.fileCount+' files, '+s.lastScan.duration.toFixed(1)+'s':'';
    ao('lout','Scan complete'+info+'\n');
    bdg('lst','idle','bi');
    document.getElementById('lscanbtn').disabled=false;
    lLoad();
  });
}

async function lCmp(){
  const older=document.getElementById('lold').value.trim();
  const newer=document.getElementById('lnew').value.trim();
  const dir  =document.getElementById('ldir').value;
  if(!older||!newer){so('lout','Set Old and New report IDs first.');return;}
  so('lout','Comparing...');
  const d=await api('POST','/api/compare',{older,newer,reportDir:dir});
  so('lout',d.output||d.error||'');
}

async function lQC(){
  const name=document.getElementById('lname').value;
  const dir =document.getElementById('ldir').value;
  so('lout','Finding last two reports with name "'+name+'"...');
  const d=await api('POST','/api/quickcompare',{reportName:name,reportDir:dir});
  so('lout',d.output||d.error||'');
}

// ═══════════════════════════════════════════════════════════════════
// AGENT TAB
// ═══════════════════════════════════════════════════════════════════
async function aStart(){
  const host=document.getElementById('ahost').value;
  const port=document.getElementById('aport').value;
  const d=await api('POST','/api/agent/start',{host,port});
  ao('aout',(d.msg||d.error||'')+'\n');
  if(!d.error){
    bdg('ast','running','br');
    document.getElementById('abtn').disabled=true;
    document.getElementById('astop').disabled=false;
  }
}
async function aStop(){
  const d=await api('POST','/api/agent/stop',{});
  ao('aout',(d.msg||d.error||'')+'\n');
  bdg('ast','stopped','bs');
  document.getElementById('abtn').disabled=false;
  document.getElementById('astop').disabled=true;
}

// ═══════════════════════════════════════════════════════════════════
// REMOTE TAB
// ═══════════════════════════════════════════════════════════════════
let rSel='';
function rAddr(){return{host:document.getElementById('rhost').value,port:document.getElementById('rport').value};}

async function rStat(){
  const{host,port}=rAddr();
  const d=await api('POST','/api/remote',{host,port,cmd:['status']});
  bdg('rst',(d.output||d.error||'?').trim(),'bi');
}
async function rList(){
  const{host,port}=rAddr();
  bdg('rst','...','bi');
  const d=await api('POST','/api/remote',{host,port,cmd:['list']});
  const rpts=parseRpts(d.output);
  buildTbl('rrpts',rpts,r=>{rSel=r;},r=>{rDoView(r);});
  bdg('rst',rpts.length+' reports','bi');
}
async function rScan(){
  const{host,port}=rAddr();
  bdg('rst','scanning...','br');so('rout','Scan started on '+host+':'+port+'...\n');
  const d=await api('POST','/api/remote',{host,port,cmd:['scan']});
  so('rout',d.output||d.error||'');bdg('rst','done','bi');
}
function rSOld(){if(rSel)document.getElementById('rold').value=rSel;}
function rSNew(){if(rSel)document.getElementById('rnew').value=rSel;}
function rView(){if(rSel)rDoView(rSel);}
async function rDoView(id){
  const{host,port}=rAddr();
  const d=await api('POST','/api/remote',{host,port,cmd:['data',id]});
  so('rout',d.output||d.error||'');
}
async function rCmp(){
  const{host,port}=rAddr();
  const older=document.getElementById('rold').value.trim();
  const newer=document.getElementById('rnew').value.trim();
  if(!older||!newer){so('rout','Set Old and New report IDs first.');return;}
  bdg('rst','comparing...','br');
  const d=await api('POST','/api/remote',{host,port,cmd:['compare',older,newer]});
  so('rout',d.output||d.error||'');bdg('rst','done','bi');
}
async function rQC(){
  const{host,port}=rAddr();
  bdg('rst','quick compare...','br');so('rout','Finding last two reports on remote agent...');
  const d=await api('POST','/api/remote',{host,port,cmd:['quickcompare']});
  so('rout',d.output||d.error||'');bdg('rst','done','bi');
}

// ═══════════════════════════════════════════════════════════════════
// HOSTS TAB
// ═══════════════════════════════════════════════════════════════════
async function hLoad(){
  const d=await api('GET','/api/hosts');
  const tb=document.getElementById('hhosts');
  tb.innerHTML='';
  (d.hosts||[]).forEach(h=>{
    const tr=document.createElement('tr');
    tr.innerHTML='<td>'+h.alias+'</td><td>'+h.address+'</td><td>'+h.port+'</td><td>'+h.path+'</td><td>'+h.reportName+'</td><td>'+(h.sshUser||'')+'</td>';
    tb.appendChild(tr);
  });
  if(d.error)so('hout','ERROR: '+d.error);
  bdg('hst',(d.hosts||[]).length+' hosts','bi');
}
async function hPing(){
  bdg('hst','pinging...','br');
  const d=await api('POST','/api/hosts/pingall',{});
  so('hout',d.output||d.error||'');bdg('hst','done','bi');
}
async function hSync(){
  bdg('hst','syncing...','br');so('hout','Syncing reports from all hosts...');
  const d=await api('POST','/api/hosts/sync',{});
  so('hout',d.output||d.error||'');bdg('hst','done','bi');
}
async function hStart(){
  bdg('hst','starting...','br');
  const d=await api('POST','/api/hosts/start',{});
  so('hout',d.msg||d.error||'');bdg('hst','done','bi');
}
async function hAll(){
  const cmdStr=document.getElementById('hcmd').value.trim();
  if(!cmdStr){so('hout','Enter a command first.');return;}
  const cmd=cmdStr.split(/\s+/);
  bdg('hst','running...','br');so('hout','Running "'+cmdStr+'" on all hosts...');
  const d=await api('POST','/api/hosts/remoteall',{cmd});
  so('hout',d.output||d.error||'');bdg('hst','done','bi');
}

// ═══════════════════════════════════════════════════════════════════
// SCHEDULE TAB
// ═══════════════════════════════════════════════════════════════════
let sSel='';
function sAddr(){return{host:document.getElementById('shost').value,port:document.getElementById('sport').value};}

function pJobs(raw){
  const rows=[];
  const lines=(raw||'').split('\n');
  for(let i=2;i<lines.length;i++){
    const l=lines[i].trim();if(!l)continue;
    const p=l.split(/\s{2,}/);
    if(p.length>=4)rows.push({n:p[0].trim(),s:p[1].trim(),c:p[2].trim(),x:p.slice(3).join('  ').trim()});
    else if(p.length===3)rows.push({n:p[0].trim(),s:p[1].trim(),c:p[2].trim(),x:'-'});
  }
  return rows;
}
function pHist(raw){
  const rows=[];
  const lines=(raw||'').split('\n');
  for(let i=2;i<lines.length;i++){
    const l=lines[i].trim();if(!l)continue;
    const p=l.split(/\s{2,}/);
    if(p.length>=4)rows.push({n:p[0].trim(),s:p[1].trim(),e:p[2].trim(),st:p.slice(3).join(' ').trim()});
  }
  return rows;
}

async function sLoad(){
  const{host,port}=sAddr();
  bdg('sst','...','bi');
  const[jd,hd]=await Promise.all([
    api('POST','/api/remote',{host,port,cmd:['schedule','list']}),
    api('POST','/api/remote',{host,port,cmd:['schedule','history']})
  ]);
  const jobs=pJobs(jd.output||'');
  const jtb=document.getElementById('sjobs');
  jtb.innerHTML='';
  jobs.forEach(j=>{
    const tr=document.createElement('tr');
    tr.innerHTML='<td>'+j.n+'</td><td>'+j.s+'</td><td>'+j.c+'</td><td>'+j.x+'</td>';
    tr.onclick=()=>{jtb.querySelectorAll('tr').forEach(x=>x.classList.remove('sel'));tr.classList.add('sel');sSel=j.n;};
    jtb.appendChild(tr);
  });
  const hist=pHist(hd.output||'');
  const htb=document.getElementById('shist');
  htb.innerHTML='';
  [...hist].reverse().forEach(h=>{
    const tr=document.createElement('tr');
    tr.innerHTML='<td>'+h.n+'</td><td>'+h.s+'</td><td>'+h.e+'</td><td>'+h.st+'</td>';
    htb.appendChild(tr);
  });
  bdg('sst',jobs.length+' jobs','bi');
}
async function sAdd(){
  const{host,port}=sAddr();
  const n=document.getElementById('sjn').value.trim();
  const c=document.getElementById('sjc').value.trim();
  const cmd=document.getElementById('sjcmd').value.trim()||'scan';
  if(!n||!c){so('sout','Name and schedule are required.');return;}
  const d=await api('POST','/api/remote',{host,port,cmd:['schedule','add',n+'|'+c+'|'+cmd]});
  so('sout',d.output||d.error||'');sLoad();
}
async function sDel(){
  if(!sSel){so('sout','Select a job first.');return;}
  const{host,port}=sAddr();
  const d=await api('POST','/api/remote',{host,port,cmd:['schedule','remove',sSel]});
  so('sout',d.output||d.error||'');sSel='';sLoad();
}

// ── init ──────────────────────────────────────────────────────────────────
lLoad();
hLoad();
</script>
</body>
</html>`
