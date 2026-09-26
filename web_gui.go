package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"sync"
	"time"
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

	// Every page and API call requires a login; the role next to each route
	// is the minimum role (see auth.go).  ro: view/compare, rw: scans and
	// schedules, admin: users.  /api/remote and /api/hosts/remoteall derive
	// the role from the command being proxied, like the agent does.
	secure := config.webTLS
	ro := func(fn http.HandlerFunc) http.HandlerFunc { return requireRole(roleRO, secure, fn) }
	rw := func(fn http.HandlerFunc) http.HandlerFunc { return requireRole(roleRW, secure, fn) }
	admin := func(fn http.HandlerFunc) http.HandlerFunc { return requireRole(roleAdmin, secure, fn) }

	mux.HandleFunc("/login", webLoginPage)
	mux.HandleFunc("/api/login", webLogin(config, secure))
	mux.HandleFunc("/api/logout", ro(webLogout))
	mux.HandleFunc("/api/me", ro(webMe))

	mux.HandleFunc("/", ro(h(webIndex)))
	mux.HandleFunc("/api/reports", ro(h(webListReports)))
	mux.HandleFunc("/api/report/", ro(h(webGetReport)))
	mux.HandleFunc("/api/compare", ro(h(webCompare)))
	mux.HandleFunc("/api/quickcompare", ro(h(webQuickCompare)))
	mux.HandleFunc("/api/status", ro(h(webStatus)))
	mux.HandleFunc("/api/hosts", ro(h(webHosts)))
	mux.HandleFunc("/api/hosts/pingall", ro(h(webPingAll)))
	mux.HandleFunc("/api/remote", ro(h(webRemote)))             // per-command role check inside
	mux.HandleFunc("/api/hosts/remoteall", ro(h(webRemoteAll))) // per-command role check inside

	mux.HandleFunc("/api/scan", rw(h(webScan)))
	mux.HandleFunc("/api/agent/start", rw(h(webAgentStart)))
	mux.HandleFunc("/api/agent/stop", rw(webAgentStop))
	mux.HandleFunc("/api/hosts/sync", rw(h(webSyncAll)))
	mux.HandleFunc("/api/hosts/start", rw(h(webStartAll)))
	mux.HandleFunc("/api/hosts/deploy", admin(h(webDeployHosts))) // GET: progress, POST: start

	mux.HandleFunc("/api/users", admin(h(webUsers)))

	if created, err := ensureDefaultUser(config.usersDB); err != nil {
		fmt.Println("ERROR - users file:", err)
		return
	} else if created {
		fmt.Printf("Created %s with the default user %q / %q - change it after logging in\n", config.usersDB, defaultAdminUser, defaultAdminPassword)
	}
	if usingDefaultPassword(config.usersDB) {
		fmt.Printf("WARNING - user %q still has the default password\n", defaultAdminUser)
	}

	addr := config.webHost + ":" + config.webPort
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	if !config.webTLS {
		fmt.Printf("Web GUI listening on http://%s (webTLS=false: passwords travel in clear text, keep this on localhost)\n", addr)
		if err := srv.ListenAndServe(); err != nil {
			fmt.Println("ERROR - web GUI:", err)
		}
		return
	}
	cert, created, err := loadOrCreateAgentCert(config.agentCert, config.agentKey)
	if err != nil {
		fmt.Println("ERROR - web GUI TLS certificate:", err)
		return
	}
	if created {
		fmt.Printf("Generated TLS certificate %s (private key %s)\n", config.agentCert, config.agentKey)
	}
	srv.TLSConfig = agentTLSConfig(cert)
	fmt.Printf("Web GUI listening on https://%s (self-signed certificate, fingerprint %s)\n", addr, certFingerprint(cert.Certificate[0]))
	if err := srv.ListenAndServeTLS("", ""); err != nil {
		fmt.Println("ERROR - web GUI:", err)
	}
}

// webRoleFor returns the role of the logged-in user for the request.
func webRoleFor(r *http.Request) (string, string) {
	ses, _ := currentSession(r)
	return ses.user, ses.role
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
	user, role := webRoleFor(r)
	tmpl.Execute(w, map[string]string{ //nolint:errcheck
		"User":       user,
		"Role":       role,
		"Path":       config.path,
		"ReportName": config.reportName,
		"ReportDir":  config.reportDir,
		"AgentHost":  config.agentHost,
		"AgentPort":  config.agentPort,
		"AgentUser":  config.agentUser,
		"AgentPass":  config.agentPassword,
		"UsersDB":    config.usersDB,
		"DeployBin":  defaultDeployBinary(),
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
	if !validReportID(id) {
		wErr(w, "missing or invalid report ID", 400)
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
		defer func() {
			if r := recover(); r != nil {
				fmt.Println("ERROR - web scan panicked:", r)
			}
			state.mu.Lock()
			state.scanRunning = false
			state.mu.Unlock()
		}()
		if err := callScan(c); err != nil {
			fmt.Println("ERROR - web scan:", err)
		}
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
	if !validReportID(req.Older) || !validReportID(req.Newer) {
		wErr(w, "older and newer required (valid report IDs)", 400)
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

func webRemote(config configInfo, w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	var req struct {
		Host     string   `json:"host"`
		Port     string   `json:"port"`
		User     string   `json:"user"`
		Password string   `json:"password"`
		Cmd      []string `json:"cmd"`
	}
	if err := wDecode(r, &req); err != nil || req.Host == "" || req.Port == "" || len(req.Cmd) == 0 {
		wErr(w, "host, port, and cmd required", 400)
		return
	}
	if _, role := webRoleFor(r); !roleAllows(role, commandRole(req.Cmd)) {
		wErr(w, "permission denied: '"+req.Cmd[0]+"' requires the "+commandRole(req.Cmd)+" role (you are "+role+")", http.StatusForbidden)
		return
	}
	c := config
	if req.User != "" {
		c.agentUser = req.User
	}
	if req.Password != "" {
		c.agentPassword = req.Password
	}
	wJSON(w, map[string]string{"output": runRemoteCommandToString(c, req.Host, req.Port, req.Cmd)})
}

// webUsers manages the local users file: {"args":["add","bob","password"]}.
// Remote agents' users are managed through /api/remote with cmd ["user", ...].
func webUsers(config configInfo, w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	var req struct {
		UsersDB string   `json:"usersDB"`
		Args    []string `json:"args"`
	}
	if err := wDecode(r, &req); err != nil || len(req.Args) == 0 {
		wErr(w, "args required", 400)
		return
	}
	path := config.usersDB
	if req.UsersDB != "" {
		path = req.UsersDB
	}
	wJSON(w, map[string]string{"output": userCommand(path, req.Args)})
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
		BinaryPath string `json:"binaryPath"`
	}
	out := make([]hj, len(hosts))
	for i, h := range hosts {
		out[i] = hj{h.alias, h.address, h.port, h.path, h.reportName, h.sshUser, h.binaryPath}
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
	wJSON(w, map[string]string{"output": pingAllString(config, hosts)})
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

// webStartAll starts the agent over SSH on one host (alias) or all hosts and
// returns the SSH output.
func webStartAll(config configInfo, w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	var req struct {
		Alias string `json:"alias"`
	}
	wDecode(r, &req) //nolint:errcheck
	var sb strings.Builder
	startAgents(config, req.Alias, &sb)
	wJSON(w, map[string]string{"output": sb.String()})
}

// webDeployHosts runs scripts/deploy.sh's job from the web GUI.  POST starts
// a deploy in the background (one at a time); GET returns its progress so the
// page can poll while scp and the restart run.
func webDeployHosts(config configInfo, w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		running, out := webDeployJob.status()
		wJSON(w, map[string]interface{}{"running": running, "output": out})
		return
	}
	if !requirePost(w, r) {
		return
	}
	var req struct {
		Alias   string `json:"alias"`
		Binary  string `json:"binary"`
		Restart *bool  `json:"restart"`
	}
	wDecode(r, &req) //nolint:errcheck
	hosts, err := parseHostsConfig(config.hostsConfig)
	if err != nil {
		wErr(w, err.Error(), 500)
		return
	}
	opts := deployOptions{binary: req.Binary, restart: req.Restart == nil || *req.Restart}
	if err := webDeployJob.start(config, hosts, req.Alias, opts); err != nil {
		wErr(w, err.Error(), 409)
		return
	}
	wJSON(w, map[string]bool{"started": true})
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
	if _, role := webRoleFor(r); !roleAllows(role, commandRole(req.Cmd)) {
		wErr(w, "permission denied: '"+req.Cmd[0]+"' requires the "+commandRole(req.Cmd)+" role (you are "+role+")", http.StatusForbidden)
		return
	}
	hosts, err := parseHostsConfig(config.hostsConfig)
	if err != nil {
		wErr(w, err.Error(), 500)
		return
	}
	wJSON(w, map[string]string{"output": remoteAllString(config, hosts, req.Cmd)})
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
  <span class="sep" style="flex:1"></span>
  <span class="sub">{{index . "User"}} ({{index . "Role"}})</span>
  <button class="btn s" onclick="logout()">Log out</button>
</header>
<div class="tabs">
  <button class="tb on"  onclick="tab('local',this)">Local</button>
  <button class="tb"     onclick="tab('agent',this)">Agent</button>
  <button class="tb"     onclick="tab('remote',this)">Remote</button>
  <button class="tb"     onclick="tab('hosts',this)">Hosts</button>
  <button class="tb"     onclick="tab('sched',this)">Schedule</button>
  <button class="tb"     data-need="admin" onclick="tab('users',this)">Users</button>
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
      <button class="btn g" id="lscanbtn" data-need="rw" onclick="lScan()">&#x25B6; Scan</button>
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
      <button class="btn g" id="abtn" data-need="rw" onclick="aStart()">&#x25B6; Start Agent</button>
      <button class="btn r" id="astop" data-need="rw" onclick="aStop()" disabled>&#x25A0; Stop</button>
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
      <label>User</label><input id="ruser" class="inp" style="width:80px" value="{{index . "AgentUser"}}">
      <label>Pass</label><input id="rpass" class="inp" type="password" style="width:100px" value="{{index . "AgentPass"}}">
      <button class="btn s" onclick="rStat()">Status</button>
      <button class="btn s" onclick="rList()">&#x27F3; List</button>
      <button class="btn g" data-need="rw" onclick="rScan()">&#x25B6; Scan</button>
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
      <button class="btn s" data-need="rw" onclick="hSync()">Sync Reports</button>
      <button class="btn s" data-need="rw" onclick="hStart(false)">Start Selected (SSH)</button>
      <button class="btn s" data-need="rw" onclick="hStart(true)">Start All (SSH)</button>
      <span class="sep"></span>
      <input id="hcmd" class="inp inp-md" placeholder="command  e.g. scan, list, status">
      <button class="btn g" onclick="hAll()">Run on All</button>
      <span class="sep"></span>
      <span id="hst" class="bdg bi">—</span>
    </div>
    <div class="row" style="margin-top:6px">
      <label>Deploy&nbsp;binary</label><input id="hbin" class="inp inp-lg" data-need="admin" value="{{index . "DeployBin"}}" title="local binary copied to each host's binaryPath (needs sshUser + binaryPath in hosts.conf)">
      <label><input type="checkbox" id="hrestart" data-need="admin" checked> restart agent</label>
      <button class="btn g" data-need="admin" onclick="hDeploy(false)">Deploy Selected</button>
      <button class="btn g" data-need="admin" onclick="hDeploy(true)">Deploy All</button>
      <span class="ct" style="opacity:.7">copies the binary, installs missing config files, pins the fingerprint (click a row to select a host)</span>
    </div>
  </div>
  <div class="card tw" style="flex:none;max-height:200px">
    <table>
      <thead><tr><th>Alias</th><th>Address</th><th>Port</th><th>Path</th><th>Report Name</th><th>SSH User</th><th>Binary Path</th></tr></thead>
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
      <label>User</label><input id="suser" class="inp" style="width:80px" value="{{index . "AgentUser"}}">
      <label>Pass</label><input id="spass" class="inp" type="password" style="width:100px" value="{{index . "AgentPass"}}">
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
          <button class="btn g" data-need="rw" onclick="sAdd()">Add</button>
          <button class="btn r" data-need="rw" onclick="sDel()">Remove</button>
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

<!-- ── USERS ─────────────────────────────────────────────────────── -->
<div id="users" class="pnl">
  <div class="card">
    <div class="row">
      <label><input type="radio" name="utgt" value="local" checked> Local users file</label>
      <input id="udb" class="inp inp-md" value="{{index . "UsersDB"}}">
      <span class="sep"></span>
      <label><input type="radio" name="utgt" value="remote"> Remote agent</label>
      <input id="uhost" class="inp inp-sm" value="{{index . "AgentHost"}}" placeholder="host">
      <input id="uport" class="inp" style="width:65px" value="{{index . "AgentPort"}}" placeholder="port">
      <input id="uuser" class="inp" style="width:80px" value="{{index . "AgentUser"}}" placeholder="login user">
      <input id="upass" class="inp" type="password" style="width:100px" value="{{index . "AgentPass"}}" placeholder="login password">
    </div>
    <div class="row mt">
      <label>Name</label><input id="uname" class="inp inp-sm" placeholder="user name">
      <label>New&nbsp;password</label><input id="unewpass" class="inp inp-sm" type="password" placeholder="min 8 chars">
      <label>Role</label><select id="urole" class="inp"><option value="ro">ro</option><option value="rw">rw</option><option value="admin">admin</option></select>
      <button class="btn s" onclick="uRun(['list'])">List Users</button>
      <button class="btn g" onclick="uAct('add')">Add</button>
      <button class="btn y" onclick="uAct('passwd')">Set Password</button>
      <button class="btn y" onclick="uAct('role')">Set Role</button>
      <button class="btn r" onclick="uAct('remove')">Remove</button>
      <button class="btn s" onclick="clr('uout')">Clear</button>
      <span class="sep"></span>
      <span id="ust" class="bdg bi">&#8212;</span>
    </div>
    <div class="row mt" style="opacity:.75">Roles: ro = view/compare reports, rw = ro + scans and schedules, admin = rw + manage users. A fresh install has "admin" / "changeme" &#8212; change it first.</div>
  </div>
  <pre class="out" id="uout" style="flex:1"></pre>
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
function v(id){const e=document.getElementById(id);return e?e.value:'';}
async function api(m,p,b){
  try{
    const o={method:m,headers:{'Content-Type':'application/json'}};
    if(b!==undefined)o.body=JSON.stringify(b);
    const r=await fetch(p,o);
    if(r.status===401){location.href='/login';return{error:'login required'};}
    const t=await r.text();
    try{return JSON.parse(t);}catch{return{error:t};}
  }catch(e){return{error:e.message};}
}

// ── output ────────────────────────────────────────────────────────────────
function so(id,t){const e=document.getElementById(id);if(e){e.textContent=t||'';e.scrollTop=e.scrollHeight;}}
function ao(id,t){const e=document.getElementById(id);if(e){e.textContent+=t;e.scrollTop=e.scrollHeight;}}
function clr(id){so(id,'');}

// ── safe table row (textContent, never innerHTML, since values come from
//    config files and remote agents) ──────────────────────────────────────
function rowOf(vals){
  const tr=document.createElement('tr');
  vals.forEach(v=>{const td=document.createElement('td');td.textContent=(v==null?'':v);tr.appendChild(td);});
  return tr;
}

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
function rAddr(){return{host:v('rhost'),port:v('rport'),user:v('ruser'),password:v('rpass')};}

async function rStat(){
  const{host,port,user,password}=rAddr();
  const d=await api('POST','/api/remote',{host,port,user,password,cmd:['status']});
  bdg('rst',(d.output||d.error||'?').trim(),'bi');
}
async function rList(){
  const{host,port,user,password}=rAddr();
  bdg('rst','...','bi');
  const d=await api('POST','/api/remote',{host,port,user,password,cmd:['list']});
  const rpts=parseRpts(d.output);
  buildTbl('rrpts',rpts,r=>{rSel=r;},r=>{rDoView(r);});
  bdg('rst',rpts.length+' reports','bi');
}
async function rScan(){
  const{host,port,user,password}=rAddr();
  bdg('rst','scanning...','br');so('rout','Scan started on '+host+':'+port+'...\n');
  const d=await api('POST','/api/remote',{host,port,user,password,cmd:['scan']});
  so('rout',d.output||d.error||'');bdg('rst','done','bi');
}
function rSOld(){if(rSel)document.getElementById('rold').value=rSel;}
function rSNew(){if(rSel)document.getElementById('rnew').value=rSel;}
function rView(){if(rSel)rDoView(rSel);}
async function rDoView(id){
  const{host,port,user,password}=rAddr();
  const d=await api('POST','/api/remote',{host,port,user,password,cmd:['data',id]});
  so('rout',d.output||d.error||'');
}
async function rCmp(){
  const{host,port,user,password}=rAddr();
  const older=document.getElementById('rold').value.trim();
  const newer=document.getElementById('rnew').value.trim();
  if(!older||!newer){so('rout','Set Old and New report IDs first.');return;}
  bdg('rst','comparing...','br');
  const d=await api('POST','/api/remote',{host,port,user,password,cmd:['compare',older,newer]});
  so('rout',d.output||d.error||'');bdg('rst','done','bi');
}
async function rQC(){
  const{host,port,user,password}=rAddr();
  bdg('rst','quick compare...','br');so('rout','Finding last two reports on remote agent...');
  const d=await api('POST','/api/remote',{host,port,user,password,cmd:['quickcompare']});
  so('rout',d.output||d.error||'');bdg('rst','done','bi');
}

// ═══════════════════════════════════════════════════════════════════
// HOSTS TAB
// ═══════════════════════════════════════════════════════════════════
let hSel='';
async function hLoad(){
  const d=await api('GET','/api/hosts');
  const tb=document.getElementById('hhosts');
  tb.innerHTML='';hSel='';
  (d.hosts||[]).forEach(h=>{
    const tr=rowOf([h.alias,h.address,h.port,h.path,h.reportName,h.sshUser||'',h.binaryPath||'']);
    tr.onclick=()=>{tb.querySelectorAll('tr').forEach(x=>x.classList.remove('sel'));tr.classList.add('sel');hSel=h.alias;bdg('hst','selected: '+h.alias,'bi');};
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
async function hStart(all){
  if(!all&&!hSel){so('hout','Click a host in the table first.');return;}
  bdg('hst','starting...','br');
  const d=await api('POST','/api/hosts/start',{alias:all?'':hSel});
  so('hout',d.output||d.error||'');bdg('hst','done','bi');
}
let _hdeploy=null;
async function hDeploy(all){
  if(!all&&!hSel){so('hout','Click a host in the table first.');return;}
  const d=await api('POST','/api/hosts/deploy',{alias:all?'':hSel,binary:v('hbin'),restart:document.getElementById('hrestart').checked});
  if(d.error){so('hout','ERROR: '+d.error);bdg('hst','error','br');return;}
  bdg('hst','deploying...','br');so('hout','Deploying'+(all?' to all hosts':' to '+hSel)+'...\n');
  if(_hdeploy)clearInterval(_hdeploy);
  _hdeploy=setInterval(async()=>{
    const s=await api('GET','/api/hosts/deploy');
    if(s.output)so('hout',s.output);
    if(!s.running){clearInterval(_hdeploy);_hdeploy=null;const bad=(s.output||'').includes('ERROR');bdg('hst',bad?'deploy failed':'deploy done',bad?'br':'bi');}
  },1000);
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
function sAddr(){return{host:v('shost'),port:v('sport'),user:v('suser'),password:v('spass')};}

// ── users tab ─────────────────────────────────────────────────────────────
async function uRun(args){
  const local=document.querySelector('input[name=utgt]:checked').value==='local';
  bdg('ust','...','bi');
  let d;
  if(local){d=await api('POST','/api/users',{usersDB:v('udb'),args});}
  else{d=await api('POST','/api/remote',{host:v('uhost'),port:v('uport'),user:v('uuser'),password:v('upass'),cmd:['user',...args]});}
  const out=(d.output||d.error||'').trim();
  so('uout',out);bdg('ust',out.startsWith('ERROR')?'error':'done',out.startsWith('ERROR')?'br':'bi');
}
function uAct(action){
  const name=v('uname').trim();
  if(!name){so('uout','Enter a user name first.');return;}
  const args=[action,name];
  if(action==='add'){args.push(v('unewpass'),v('urole'));}
  else if(action==='passwd'){args.push(v('unewpass'));}
  else if(action==='role'){args.push(v('urole'));}
  uRun(args);
}

// ── login / roles ─────────────────────────────────────────────────────────
const RANK={ro:1,rw:2,admin:3};
async function logout(){await api('POST','/api/logout',{});location.href='/login';}
async function applyRole(){
  const me=await api('GET','/api/me');
  const mine=RANK[me.role]||0;
  document.querySelectorAll('[data-need]').forEach(el=>{
    if(mine<RANK[el.dataset.need]){el.disabled=true;el.title='requires the '+el.dataset.need+' role';el.style.opacity=.4;}
  });
}
applyRole();

// The agent returns padded text tables.  Cron expressions and timestamps
// contain spaces, so rows are matched by column shape rather than split on
// whitespace (this also copes with long job names that eat the padding).
const TS='\\d{4}-\\d{2}-\\d{2} \\d{2}:\\d{2}:\\d{2}';
const JOB_RE=new RegExp('^(\\S+)\\s+(.+?)\\s+(\\S+)\\s+('+TS+'|-)\\s*$');
const HIST_RE=new RegExp('^(\\S+)\\s+('+TS+')\\s+('+TS+'|-)\\s+(.*?)\\s*$');
function pJobs(raw){
  const rows=[];
  for(const line of (raw||'').split('\n')){
    const m=JOB_RE.exec(line.trim());
    if(!m||m[1]==='NAME')continue;
    rows.push({n:m[1],s:m[2],c:m[3],x:m[4]});
  }
  return rows;
}
function pHist(raw){
  const rows=[];
  for(const line of (raw||'').split('\n')){
    const m=HIST_RE.exec(line.trim());
    if(!m)continue;
    rows.push({n:m[1],s:m[2],e:m[3],st:m[4]});
  }
  return rows;
}

async function sLoad(){
  const{host,port,user,password}=sAddr();
  bdg('sst','...','bi');
  const[jd,hd]=await Promise.all([
    api('POST','/api/remote',{host,port,user,password,cmd:['schedule','list']}),
    api('POST','/api/remote',{host,port,user,password,cmd:['schedule','history']})
  ]);
  const jobs=pJobs(jd.output||'');
  const jtb=document.getElementById('sjobs');
  jtb.innerHTML='';
  jobs.forEach(j=>{
    const tr=rowOf([j.n,j.s,j.c,j.x]);
    tr.onclick=()=>{jtb.querySelectorAll('tr').forEach(x=>x.classList.remove('sel'));tr.classList.add('sel');sSel=j.n;};
    jtb.appendChild(tr);
  });
  const hist=pHist(hd.output||'');
  const htb=document.getElementById('shist');
  htb.innerHTML='';
  [...hist].reverse().forEach(h=>{
    const tr=rowOf([h.n,h.s,h.e,h.st]);
    htb.appendChild(tr);
  });
  bdg('sst',jobs.length+' jobs','bi');
}
async function sAdd(){
  const{host,port,user,password}=sAddr();
  const n=document.getElementById('sjn').value.trim();
  const c=document.getElementById('sjc').value.trim();
  const cmd=document.getElementById('sjcmd').value.trim()||'scan';
  if(!n||!c){so('sout','Name and schedule are required.');return;}
  const d=await api('POST','/api/remote',{host,port,user,password,cmd:['schedule','add',n+'|'+c+'|'+cmd]});
  so('sout',d.output||d.error||'');sLoad();
}
async function sDel(){
  if(!sSel){so('sout','Select a job first.');return;}
  const{host,port,user,password}=sAddr();
  const d=await api('POST','/api/remote',{host,port,user,password,cmd:['schedule','remove',sSel]});
  so('sout',d.output||d.error||'');sSel='';sLoad();
}

// ── init ──────────────────────────────────────────────────────────────────
lLoad();
hLoad();
</script>
</body>
</html>`
