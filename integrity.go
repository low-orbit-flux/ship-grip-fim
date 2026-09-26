/*

This file is messy but I wanted all of the functionality and args need to be
handled in a specific order due to precedence, dereferencing, and other reasons.

    - hardcoded values are the default
    - config file values override hardcoded values
    - CLI arg values override both config file and hardcoded values

    - order matters
    - dereferencing at the right time matters

*/

package main

import (
	"bufio"
	"fmt"
	//"io"
	"flag"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// version is set at build time by scripts/build.sh (-X main.version=...).
var version = "dev"

type configInfo struct {
	reportDir              string
	dataSource             string
	reportName             string
	host                   string
	path                   string
	paraCount              int
	removeBasePath         bool
	configFile             string
	ignorePathConfig       string
	ignorePathNoWalkConfig string
	ignorePath             []*regexp.Regexp
	ignorePathNoWalk       []*regexp.Regexp
	agentHost              string
	agentPort              string
	hostsConfig            string
	scheduleConfig         string
	exporterHost           string
	exporterPort           string
	webHost                string
	webPort                string
	agentCert              string // agent TLS certificate (PEM), generated on first start
	agentKey               string // agent TLS private key (PEM)
	knownAgents            string // client-side pinned agent fingerprints (like ssh known_hosts)
	trustNewAgents         bool   // pin unknown agents on first connection (TOFU) instead of refusing
	usersDB                string // agent users file (name:bcrypt-hash)
	agentUser              string // credentials used when connecting to agents
	agentPassword          string
	webTLS                 bool // serve the web GUI over HTTPS with the agent certificate
	test                   string
}

func usage() {
	usageString := `
Usage:
    ship-grip-fim scan
    ship-grip-fim list
    ship-grip-fim data <ID>
    ship-grip-fim compare <ID> <ID>
    ship-grip-fim agent
    ship-grip-fim gui
    ship-grip-fim remote <host> <port> <command> [args...]
    ship-grip-fim hosts
    ship-grip-fim pingall
    ship-grip-fim sync [alias]
    ship-grip-fim remoteall <command> [args...]
    ship-grip-fim start [alias]
    ship-grip-fim deploy [alias] [--no-restart] [--binary=PATH]
    ship-grip-fim exporter
    ship-grip-fim webgui
    ship-grip-fim quickcompare
    ship-grip-fim user list|add <name> <password> [ro|rw|admin]|passwd <name> <password>|role <name> <role>|remove <name>
    ship-grip-fim fingerprint
    ship-grip-fim version

    ship-grip-fim --removeBasePath=true compare default_adhoc_report_2025-09-15_23-09-49 default_adhoc_report_2025-09-15_23-10-28

    scan      - Checksum every file under --path recursively and save a timestamped report.
    list      - List all stored reports.
    data      - Dump all path/checksum pairs from a report.
    compare   - Compare two reports.  Older report ID first, newer second.
                Use --removeBasePath=true when the filesystem was remounted at a different path.
    agent     - Start the remote agent server (listens on agentHost:agentPort from config).
    exporter  - Start the Prometheus metrics exporter (scrapes all agents in hosts.conf,
                listens on exporterHost:exporterPort, default 0.0.0.0:9110).
    webgui    - Start the browser-based GUI (same features as desktop GUI).
                Listens on webHost:webPort, default localhost:8090.
    quickcompare - Find the two most recent reports sharing --reportName and compare them.
    remote    - Connect to a running agent and run a command.
                Commands: scan, list, data <ID>, fetch <ID>, compare <ID> <ID>, status
                Example:  ship-grip-fim remote localhost 8080 compare id1 id2
    hosts     - List all remote hosts from the hosts config file.
    pingall   - Check connectivity and status of all configured agents.
    sync      - Pull reports from one (alias) or all configured agents into
                <reportDir>/remote/<alias>/.  Already-present files are skipped.
    remoteall - Run a command on all configured agents in parallel.
                Example:  ship-grip-fim remoteall scan
    gui       - Open the graphical interface (requires a display).
    start     - Start the agent on one (alias) or all remote hosts via SSH.
                Requires sshUser and binaryPath set in hosts.conf.
    deploy    - Copy the binary (default build/ship-grip-fim, else this
                executable) to one (alias) or all hosts over SSH, install the
                config files if missing, pin the agent's TLS fingerprint in
                knownAgents and restart the agent.  Same as scripts/deploy.sh;
                also available on the Hosts tab of both GUIs.
    user      - Manage the local agent users file (usersDB).  To manage a remote
                agent's users:  ship-grip-fim remote <host> <port> user add bob s3cretpass
    fingerprint - Print the local agent's TLS certificate fingerprint (for
                pre-pinning it in known_agents on other machines).

    Security: agents and the web GUI speak TLS 1.3 and require a login (sent
    with agentUser/agentPassword from the config, or SGF_AGENT_PASSWORD env).
    Roles: ro = view/compare reports, rw = ro + scans and schedules,
    admin = rw + manage users.  A fresh install creates "admin" / "changeme".

	------------------------------------------------

    NOTE - Flags need to go before positional args



	`
	fmt.Print(usageString)
	flag.PrintDefaults()
	log.Fatal("\n\nExiting ...")
}

func main() {

	/*
		        Hardcoded settings, these will be used if there are no args and nothing
			    	is found in the config file.
				Decided NOT to make these GLOBAL so that in the future it will be easier
				to have separate running jobs that use different structures in parallel.
	*/
	config := configInfo{
		reportDir:              "~/integrity_reports", // was originally global ("~" is expanded below)
		dataSource:             "file",                // was originally global
		reportName:             "default_adhoc_report",
		host:                   "duck-puppy",
		path:                   "/storage1",
		paraCount:              8,
		removeBasePath:         false,
		configFile:             "integrity.conf",
		ignorePathConfig:       "integrity_ignore.cfg",
		ignorePathNoWalkConfig: "integrity_ignore_no_walk.cfg",
		ignorePath:             []*regexp.Regexp{},
		ignorePathNoWalk:       []*regexp.Regexp{},
		agentHost:              "localhost",
		agentPort:              "8080",
		hostsConfig:            "hosts.conf",
		scheduleConfig:         "schedule.conf",
		exporterHost:           "0.0.0.0",
		exporterPort:           "9110",
		webHost:                "localhost",
		webPort:                "8090",
		agentCert:              "agent.crt",
		agentKey:               "agent.key",
		knownAgents:            "known_agents",
		trustNewAgents:         true,
		usersDB:                "users.db",
		agentUser:              defaultAdminUser,
		agentPassword:          defaultAdminPassword,
		webTLS:                 true,
	}

	// named params, "----" is default and won't override config file
	reportDir_ptr := flag.String("reportDir", "----", "report dir")
	dataSource_ptr := flag.String("dataSource", "----", "data source type")
	reportName_ptr := flag.String("reportName", "----", "report name")
	host_ptr := flag.String("host", "----", "host")
	path_ptr := flag.String("path", "----", "path")
	paraCount_ptr := flag.String("paraCount", "----", "parallel instances ( CPU cores to use )")
	removeBasePath_ptr := flag.String("removeBasePath", "----", "remove base path")
	configFile_ptr := flag.String("configFile", "----", "config file path")
	ignorePathConfig_ptr := flag.String("ignorePathConfig", "----", "ignore path config file path")
	ignorePathNoWalkConfig_ptr := flag.String("ignorePathNoWalkConfig", "----", "ignore path no walk config file path")
	agentHost_ptr := flag.String("agentHost", "----", "agent server bind address / interface")
	agentPort_ptr := flag.String("agentPort", "----", "agent server port")
	hostsConfig_ptr := flag.String("hostsConfig", "----", "remote hosts config file path")
	scheduleConfig_ptr := flag.String("scheduleConfig", "----", "agent schedule config file path")
	exporterHost_ptr := flag.String("exporterHost", "----", "Prometheus exporter bind address")
	exporterPort_ptr := flag.String("exporterPort", "----", "Prometheus exporter port")
	webHost_ptr := flag.String("webHost", "----", "web GUI bind address")
	webPort_ptr := flag.String("webPort", "----", "web GUI port")
	agentCert_ptr := flag.String("agentCert", "----", "agent TLS certificate file")
	agentKey_ptr := flag.String("agentKey", "----", "agent TLS private key file")
	knownAgents_ptr := flag.String("knownAgents", "----", "pinned agent fingerprints file")
	trustNewAgents_ptr := flag.String("trustNewAgents", "----", "trust unknown agents on first connection (true/false)")
	usersDB_ptr := flag.String("usersDB", "----", "agent users file")
	agentUser_ptr := flag.String("agentUser", "----", "user name for connecting to agents")
	agentPassword_ptr := flag.String("agentPassword", "----", "password for connecting to agents (prefer SGF_AGENT_PASSWORD env)")
	webTLS_ptr := flag.String("webTLS", "----", "serve the web GUI over HTTPS (true/false)")

	flag.Usage = usage
	flag.Parse() // args are pointers because this function needs it
	positional := flag.Args()
	action := ""
	if len(positional) >= 1 {
		action = positional[0] // scan, list, data, compare, agent, remote
	} else {
		usage()
	}
	id1 := ""
	id2 := ""
	switch action {
	case "data":
		if len(positional) >= 2 {
			id1 = positional[1]
		} else {
			usage()
		}
	case "compare":
		if len(positional) >= 3 {
			id1 = positional[1]
			id2 = positional[2]
		} else {
			usage()
		}
	case "remote":
		if len(positional) < 4 {
			usage()
		}
	case "remoteall":
		if len(positional) < 2 {
			usage()
		}
	case "user":
		if len(positional) < 2 {
			usage()
		}
	}

	// only this one gets assigned/derefferenced here ( before reading config file )
	if *configFile_ptr != "----" {
		config.configFile = *configFile_ptr
	}

	// Read settings from config file, these may be overridden by any commandline args
	configData, err := os.ReadFile(config.configFile)
	if err != nil {
		fmt.Println("ERROR - can't read config file, using defaults")
	}
	cf := string(configData)
	re1, err := regexp.Compile(`(?m)^(reportName)="(.*)"`)
	if r := re1.FindAllStringSubmatch(cf, -1); r != nil {
		config.reportName = r[0][2]
	}
	re1, err = regexp.Compile(`(?m)^(host)="(.*)"`)
	if r := re1.FindAllStringSubmatch(cf, -1); r != nil {
		config.host = r[0][2]
	}
	re1, err = regexp.Compile(`(?m)^(path)="(.*)"`)
	if r := re1.FindAllStringSubmatch(cf, -1); r != nil {
		config.path = r[0][2]
	}
	re1, err = regexp.Compile(`(?m)^(reportDir)="(.*)"`)
	if r := re1.FindAllStringSubmatch(cf, -1); r != nil {
		config.reportDir = r[0][2]
	}
	re1, err = regexp.Compile(`(?m)^(dataSource)="(.*)"`)
	if r := re1.FindAllStringSubmatch(cf, -1); r != nil {
		config.dataSource = r[0][2]
	}
	re1, err = regexp.Compile(`(?m)^(paraCount)="(.*)"`)
	if r := re1.FindAllStringSubmatch(cf, -1); r != nil {
		config.paraCount, err = strconv.Atoi(r[0][2])
	}
	re1, err = regexp.Compile(`(?m)^(removeBasePath)="(.*)"`)
	if r := re1.FindAllStringSubmatch(cf, -1); r != nil {
		config.removeBasePath, _ = strconv.ParseBool(r[0][2])
	}
	re1, err = regexp.Compile(`(?m)^(ignorePathConfig)="(.*)"`)
	if r := re1.FindAllStringSubmatch(cf, -1); r != nil {
		config.ignorePathConfig = r[0][2]
	}
	re1, err = regexp.Compile(`(?m)^(ignorePathNoWalkConfig)="(.*)"`)
	if r := re1.FindAllStringSubmatch(cf, -1); r != nil {
		config.ignorePathNoWalkConfig = r[0][2]
	}
	re1, err = regexp.Compile(`(?m)^(agentHost)="(.*)"`)
	if r := re1.FindAllStringSubmatch(cf, -1); r != nil {
		config.agentHost = r[0][2]
	}
	re1, err = regexp.Compile(`(?m)^(agentPort)="(.*)"`)
	if r := re1.FindAllStringSubmatch(cf, -1); r != nil {
		config.agentPort = r[0][2]
	}
	re1, err = regexp.Compile(`(?m)^(hostsConfig)="(.*)"`)
	if r := re1.FindAllStringSubmatch(cf, -1); r != nil {
		config.hostsConfig = r[0][2]
	}
	re1, err = regexp.Compile(`(?m)^(scheduleConfig)="(.*)"`)
	if r := re1.FindAllStringSubmatch(cf, -1); r != nil {
		config.scheduleConfig = r[0][2]
	}
	re1, err = regexp.Compile(`(?m)^(exporterHost)="(.*)"`)
	if r := re1.FindAllStringSubmatch(cf, -1); r != nil {
		config.exporterHost = r[0][2]
	}
	re1, err = regexp.Compile(`(?m)^(exporterPort)="(.*)"`)
	if r := re1.FindAllStringSubmatch(cf, -1); r != nil {
		config.exporterPort = r[0][2]
	}
	re1, err = regexp.Compile(`(?m)^(webHost)="(.*)"`)
	if r := re1.FindAllStringSubmatch(cf, -1); r != nil {
		config.webHost = r[0][2]
	}
	re1, err = regexp.Compile(`(?m)^(webPort)="(.*)"`)
	if r := re1.FindAllStringSubmatch(cf, -1); r != nil {
		config.webPort = r[0][2]
	}
	for key, dst := range map[string]*string{
		"agentCert": &config.agentCert, "agentKey": &config.agentKey, "knownAgents": &config.knownAgents,
		"usersDB": &config.usersDB, "agentUser": &config.agentUser, "agentPassword": &config.agentPassword,
	} {
		re1, _ = regexp.Compile(`(?m)^(` + key + `)="(.*)"`)
		if r := re1.FindAllStringSubmatch(cf, -1); r != nil {
			*dst = r[0][2]
		}
	}
	re1, _ = regexp.Compile(`(?m)^(trustNewAgents)="(.*)"`)
	if r := re1.FindAllStringSubmatch(cf, -1); r != nil {
		config.trustNewAgents, _ = strconv.ParseBool(r[0][2])
	}
	re1, _ = regexp.Compile(`(?m)^(webTLS)="(.*)"`)
	if r := re1.FindAllStringSubmatch(cf, -1); r != nil {
		config.webTLS, _ = strconv.ParseBool(r[0][2])
	}
	_ = err

	/*
		- CLI args override config file options here
		- only if they don't have the default value ("----"), meaning they were actually set
		- also need to be de-referenced and converted
	*/
	if *reportDir_ptr != "----" {
		config.reportDir = *reportDir_ptr
	}
	if *dataSource_ptr != "----" {
		config.dataSource = *dataSource_ptr
	}
	if *reportName_ptr != "----" {
		config.reportName = *reportName_ptr
	}
	if *host_ptr != "----" {
		config.host = *host_ptr
	}
	if *path_ptr != "----" {
		config.path = *path_ptr
	}
	if *paraCount_ptr != "----" {
		config.paraCount, _ = strconv.Atoi(*paraCount_ptr)
	}
	if *removeBasePath_ptr != "----" {
		config.removeBasePath, _ = strconv.ParseBool(*removeBasePath_ptr)
	}
	if *ignorePathConfig_ptr != "----" {
		config.ignorePathConfig = *ignorePathConfig_ptr
	}
	if *ignorePathNoWalkConfig_ptr != "----" {
		config.ignorePathNoWalkConfig = *ignorePathNoWalkConfig_ptr
	}
	if *agentHost_ptr != "----" {
		config.agentHost = *agentHost_ptr
	}
	if *agentPort_ptr != "----" {
		config.agentPort = *agentPort_ptr
	}
	if *hostsConfig_ptr != "----" {
		config.hostsConfig = *hostsConfig_ptr
	}
	if *scheduleConfig_ptr != "----" {
		config.scheduleConfig = *scheduleConfig_ptr
	}
	if *exporterHost_ptr != "----" {
		config.exporterHost = *exporterHost_ptr
	}
	if *exporterPort_ptr != "----" {
		config.exporterPort = *exporterPort_ptr
	}
	if *webHost_ptr != "----" {
		config.webHost = *webHost_ptr
	}
	if *webPort_ptr != "----" {
		config.webPort = *webPort_ptr
	}

	if *agentCert_ptr != "----" {
		config.agentCert = *agentCert_ptr
	}
	if *agentKey_ptr != "----" {
		config.agentKey = *agentKey_ptr
	}
	if *knownAgents_ptr != "----" {
		config.knownAgents = *knownAgents_ptr
	}
	if *trustNewAgents_ptr != "----" {
		config.trustNewAgents, _ = strconv.ParseBool(*trustNewAgents_ptr)
	}
	if *usersDB_ptr != "----" {
		config.usersDB = *usersDB_ptr
	}
	if *agentUser_ptr != "----" {
		config.agentUser = *agentUser_ptr
	}
	if *agentPassword_ptr != "----" {
		config.agentPassword = *agentPassword_ptr
	}
	if *webTLS_ptr != "----" {
		config.webTLS, _ = strconv.ParseBool(*webTLS_ptr)
	}
	// The environment wins over the config file so the password need not be
	// written to disk (useful for the exporter and cron jobs).
	if p := os.Getenv("SGF_AGENT_PASSWORD"); p != "" {
		config.agentPassword = p
	}

	config.reportDir = expandHome(config.reportDir)
	if config.paraCount < 1 {
		fmt.Println("WARN - paraCount must be >= 1, using 1")
		config.paraCount = 1
	}

	loadConfigsOther(config.ignorePathConfig, &config.ignorePath)             // load other configs ( exclude files)
	loadConfigsOther(config.ignorePathNoWalkConfig, &config.ignorePathNoWalk) // load other configs ( exclude files)

	switch action {
	case "scan":
		if err := callScan(config); err != nil {
			fmt.Println("ERROR -", err)
			os.Exit(1)
		}
	case "list":
		fmt.Print(listReports(config))

	case "data":
		if err := listReportData(config, id1); err != nil {
			fmt.Println("ERROR -", err)
			os.Exit(1)
		}

	case "compare":
		if err := compareReports(config, id1, id2); err != nil {
			fmt.Println("ERROR -", err)
			os.Exit(1)
		}

	case "agent":
		startAgentServer(config)

	case "gui":
		startGUI(config)

	case "remote":
		// positional: remote <host> <port> <cmd> [args...]
		remoteHost := positional[1]
		remotePort := positional[2]
		remoteArgs := positional[3:]
		runRemoteCommand(config, remoteHost, remotePort, remoteArgs)

	case "hosts":
		cmdHosts(config)

	case "pingall":
		cmdPingAll(config)

	case "sync":
		alias := ""
		if len(positional) >= 2 {
			alias = positional[1]
		}
		syncReports(config, alias)

	case "remoteall":
		cmdRemoteAll(config, positional[1:])

	case "start":
		alias := ""
		if len(positional) >= 2 {
			alias = positional[1]
		}
		cmdStartAgent(config, alias)

	case "deploy":
		if err := cmdDeploy(config, positional[1:]); err != nil {
			fmt.Println("ERROR -", err)
			os.Exit(1)
		}

	case "exporter":
		startExporter(config)

	case "webgui":
		startWebGUI(config)

	case "quickcompare":
		if err := cmdQuickCompare(config); err != nil {
			fmt.Println("ERROR -", err)
			os.Exit(1)
		}

	case "user":
		out := userCommand(config.usersDB, positional[1:])
		fmt.Print(out)
		if strings.HasPrefix(out, "ERROR") {
			os.Exit(1)
		}

	case "fingerprint":
		if err := cmdFingerprint(config); err != nil {
			fmt.Println("ERROR -", err)
			os.Exit(1)
		}

	case "version":
		fmt.Println("ship-grip-fim", version)

	default:
		usage()

	}
}

// expandHome replaces a leading "~" with the user's home directory.  Go does
// not expand it, so without this a directory literally named "~" is created.
func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[1:])
		}
	}
	return p
}

// callScan walks config.path, checksums every file, and writes a new report.
// It is called by the CLI, the agent, the scheduler, and both GUIs.
func callScan(config configInfo) error {
	if _, err := os.Stat(config.path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("directory does not exist: %s", config.path)
		}
		return err
	}
	fileMap := SafeFileMap{v: make(map[string]string)}
	fmt.Printf("ParaCount: %v\n\n", config.paraCount)
	start := time.Now()
	parallelFileCheck(config, &fileMap)
	duration := time.Since(start)
	if err := saveToDB(config, &fileMap); err != nil {
		return err
	}
	// Record stats for the metrics endpoint.
	state.mu.Lock()
	state.lastScan = &scanStats{
		ReportName: config.reportName,
		Timestamp:  time.Now(),
		FileCount:  len(fileMap.v),
		Duration:   duration,
	}
	state.mu.Unlock()
	return nil
}

// loadConfigsOther loads one regex per non-comment line of cPath into cList.
// Lines that fail to compile are reported and skipped rather than being
// appended as nil (which would panic on first use).
func loadConfigsOther(cPath string, cList *[]*regexp.Regexp) {
	file, err := os.Open(cPath)
	if err != nil {
		fmt.Println("ERROR - opening file:", err)
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue // skip empty or commented lines
		}
		re, err := regexp.Compile(line)
		if err != nil {
			fmt.Printf("ERROR - %s line %d: bad regex %q: %v (skipped)\n", cPath, lineNum, line, err)
			continue
		}
		*cList = append(*cList, re)
	}

	if err := scanner.Err(); err != nil {
		fmt.Println("ERROR - reading file:", err)
	}
}
