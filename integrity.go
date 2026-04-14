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
	"fmt"
	"bufio"
	//"io"
	"io/ioutil"
	"log"
	"os"
	"flag"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type configInfo struct {
	reportDir string
    dataSource string
    reportName string
    host string
    path string
    paraCount int
    removeBasePath bool
    configFile string
	ignorePathConfig string
	ignorePathNoWalkConfig string
	ignorePath []*regexp.Regexp
	ignorePathNoWalk []*regexp.Regexp
	agentHost      string
	agentPort      string
	hostsConfig    string
	scheduleConfig string
	exporterHost   string
	exporterPort   string
	test string
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
    ship-grip-fim exporter

    ship-grip-fim --removeBasePath=true compare default_adhoc_report_2025-09-15_23:09:49 default_adhoc_report_2025-09-15_23:10:28

    scan      - Checksum every file under --path recursively and save a timestamped report.
    list      - List all stored reports.
    data      - Dump all path/checksum pairs from a report.
    compare   - Compare two reports.  Older report ID first, newer second.
                Use --removeBasePath=true when the filesystem was remounted at a different path.
    agent     - Start the remote agent server (listens on agentHost:agentPort from config).
    exporter  - Start the Prometheus metrics exporter (scrapes all agents in hosts.conf,
                listens on exporterHost:exporterPort, default 0.0.0.0:9110).
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

	------------------------------------------------

    NOTE - Flags need to go before positional args



	`
	fmt.Printf(usageString)
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
		reportDir:  "~/integirty_reports",    // was originally global
    	dataSource: "file",                   // was originally global
    	reportName:  "default_adhoc_report",
    	host:  "duck-puppy",
  		path:  "/storage1",
    	paraCount:  8,
		removeBasePath:  false,
    	configFile:  "integrity.conf",
		ignorePathConfig: "integrity_ignore.cfg",
		ignorePathNoWalkConfig: "integrity_ignore_no_walk.cfg",
	    ignorePath:        []*regexp.Regexp{},
	    ignorePathNoWalk:  []*regexp.Regexp{},
		agentHost:      "localhost",
		agentPort:      "8080",
		hostsConfig:    "hosts.conf",
		scheduleConfig: "schedule.conf",
		exporterHost:   "0.0.0.0",
		exporterPort:   "9110",
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
	agentHost_ptr      := flag.String("agentHost",      "----", "agent server bind address / interface")
	agentPort_ptr      := flag.String("agentPort",      "----", "agent server port")
	hostsConfig_ptr    := flag.String("hostsConfig",    "----", "remote hosts config file path")
	scheduleConfig_ptr := flag.String("scheduleConfig", "----", "agent schedule config file path")
	exporterHost_ptr   := flag.String("exporterHost",   "----", "Prometheus exporter bind address")
	exporterPort_ptr   := flag.String("exporterPort",   "----", "Prometheus exporter port")

    flag.Usage = usage
	flag.Parse()                 // args are pointers because this function needs it
	positional := flag.Args()
	action := ""
	if len(positional) >= 1 {
	    action = positional[0] // scan, list, data, compare, agent, remote
    } else { usage() }
    id1 := ""
	id2 := ""
	switch action {
		case "data":
		    if len(positional) >= 2 { id1 = positional[1] } else { usage() }
		case "compare":
		    if len(positional) >= 3 {
				id1 = positional[1]
				id2 = positional[2]
			} else { usage() }
		case "remote":
		    if len(positional) < 4 { usage() }
		case "remoteall":
		    if len(positional) < 2 { usage() }
	}

	// only this one gets assigned/derefferenced here ( before reading config file )
	if *configFile_ptr != "----" { config.configFile = *configFile_ptr }

    // Read settings from config file, these may be overridden by any commandline args
 	configData, err := ioutil.ReadFile(config.configFile)
    if err != nil {
        fmt.Println( "ERROR - can't read config file, using defaults")
    }
    cf := string(configData)
    re1, err := regexp.Compile(`(?m)^(reportName)="(.*)"`)
    if r := re1.FindAllStringSubmatch(cf, -1); r != nil { config.reportName = r[0][2] }
	re1, err = regexp.Compile(`(?m)^(host)="(.*)"`)
    if r := re1.FindAllStringSubmatch(cf, -1); r != nil { config.host = r[0][2] }
	re1, err = regexp.Compile(`(?m)^(path)="(.*)"`)
    if r := re1.FindAllStringSubmatch(cf, -1); r != nil { config.path = r[0][2]	}
	re1, err = regexp.Compile(`(?m)^(reportDir)="(.*)"`)
    if r := re1.FindAllStringSubmatch(cf, -1); r != nil { config.reportDir = r[0][2] }
	re1, err = regexp.Compile(`(?m)^(dataSource)="(.*)"`)
	if r := re1.FindAllStringSubmatch(cf, -1); r != nil { config.dataSource = r[0][2] }
	re1, err = regexp.Compile(`(?m)^(paraCount)="(.*)"`)
	if r := re1.FindAllStringSubmatch(cf, -1); r != nil { config.paraCount, err = strconv.Atoi(r[0][2]) }
	re1, err = regexp.Compile(`(?m)^(removeBasePath)="(.*)"`)
	if r := re1.FindAllStringSubmatch(cf, -1); r != nil { config.removeBasePath, _ = strconv.ParseBool(r[0][2]) }
	re1, err = regexp.Compile(`(?m)^(ignorePathConfig)="(.*)"`)
	if r := re1.FindAllStringSubmatch(cf, -1); r != nil { config.ignorePathConfig = r[0][2] }
	re1, err = regexp.Compile(`(?m)^(ignorePathNoWalkConfig)="(.*)"`)
	if r := re1.FindAllStringSubmatch(cf, -1); r != nil { config.ignorePathNoWalkConfig = r[0][2] }
	re1, err = regexp.Compile(`(?m)^(agentHost)="(.*)"`)
	if r := re1.FindAllStringSubmatch(cf, -1); r != nil { config.agentHost = r[0][2] }
	re1, err = regexp.Compile(`(?m)^(agentPort)="(.*)"`)
	if r := re1.FindAllStringSubmatch(cf, -1); r != nil { config.agentPort = r[0][2] }
	re1, err = regexp.Compile(`(?m)^(hostsConfig)="(.*)"`)
	if r := re1.FindAllStringSubmatch(cf, -1); r != nil { config.hostsConfig = r[0][2] }
	re1, err = regexp.Compile(`(?m)^(scheduleConfig)="(.*)"`)
	if r := re1.FindAllStringSubmatch(cf, -1); r != nil { config.scheduleConfig = r[0][2] }
	re1, err = regexp.Compile(`(?m)^(exporterHost)="(.*)"`)
	if r := re1.FindAllStringSubmatch(cf, -1); r != nil { config.exporterHost = r[0][2] }
	re1, err = regexp.Compile(`(?m)^(exporterPort)="(.*)"`)
	if r := re1.FindAllStringSubmatch(cf, -1); r != nil { config.exporterPort = r[0][2] }
	_ = err


    /*
		- CLI args override config file options here
		- only if they don't have the default value ("----"), meaning they were actually set
		- also need to be de-referenced and converted
    */
	if *reportDir_ptr != "----"  { config.reportDir  = *reportDir_ptr }
	if *dataSource_ptr != "----" { config.dataSource = *dataSource_ptr }
	if *reportName_ptr != "----" { config.reportName = *reportName_ptr }
	if *host_ptr != "----"       { config.host       = *host_ptr }
	if *path_ptr != "----"       { config.path       = *path_ptr }
	if *paraCount_ptr != "----"  { config.paraCount, _ = strconv.Atoi(*paraCount_ptr) }
    if *removeBasePath_ptr != "----"         { config.removeBasePath, _ = strconv.ParseBool(*removeBasePath_ptr) }
	if *ignorePathConfig_ptr != "----"       { config.ignorePathConfig       = *ignorePathConfig_ptr }
	if *ignorePathNoWalkConfig_ptr != "----" { config.ignorePathNoWalkConfig = *ignorePathNoWalkConfig_ptr }
	if *agentHost_ptr != "----"              { config.agentHost              = *agentHost_ptr }
	if *agentPort_ptr != "----"              { config.agentPort              = *agentPort_ptr }
	if *hostsConfig_ptr != "----"            { config.hostsConfig            = *hostsConfig_ptr }
	if *scheduleConfig_ptr != "----"        { config.scheduleConfig         = *scheduleConfig_ptr }
	if *exporterHost_ptr != "----"          { config.exporterHost           = *exporterHost_ptr }
	if *exporterPort_ptr != "----"          { config.exporterPort           = *exporterPort_ptr }



    loadConfigsOther(config.ignorePathConfig, &config.ignorePath)            // load other configs ( exclude files)
	loadConfigsOther(config.ignorePathNoWalkConfig, &config.ignorePathNoWalk)// load other configs ( exclude files)


    switch action {
		case "scan":
            callScan(config)
		case "list":
			fmt.Print(listReports(config))

		case "data":
			listReportData(config, id1)

		case "compare":
    	    compareReports(config, id1, id2)

		case "agent":
		    startAgentServer(config)

		case "gui":
		    startGUI(config)

		case "remote":
		    // positional: remote <host> <port> <cmd> [args...]
		    remoteHost := positional[1]
		    remotePort := positional[2]
		    remoteArgs := positional[3:]
		    runRemoteCommand(remoteHost, remotePort, remoteArgs)

		case "hosts":
		    cmdHosts(config)

		case "pingall":
		    cmdPingAll(config)

		case "sync":
		    alias := ""
		    if len(positional) >= 2 { alias = positional[1] }
		    syncReports(config, alias)

		case "remoteall":
		    cmdRemoteAll(config, positional[1:])

		case "start":
		    alias := ""
		    if len(positional) >= 2 { alias = positional[1] }
		    cmdStartAgent(config, alias)

		case "exporter":
		    startExporter(config)

		default:
			usage()

    }
}

// this needs to be a function called by other
func callScan(config configInfo){
	if _, err := os.Stat(config.path); err != nil {
		if os.IsNotExist(err) { fmt.Println("ERROR - Directory does not exist")
		} else { fmt.Println("ERROR - ", err) }
	} else {
		fileMap := SafeFileMap{v: make(map[string]string)}
		fmt.Printf("ParaCount: %v\n\n", config.paraCount)
		start := time.Now()
		parallelFileCheck(config, &fileMap)
		duration := time.Since(start)
		fmt.Printf("test %s, %s, %s, %s", config.reportName, config.host, config.path, config.reportDir)
		saveToDB(config, &fileMap)
		// Record stats for the metrics endpoint.
		state.mu.Lock()
		state.lastScan = &scanStats{
			ReportName: config.reportName,
			Timestamp:  time.Now(),
			FileCount:  len(fileMap.v),
			Duration:   duration,
		}
		state.mu.Unlock()
	}
}


func loadConfigsOther(cPath string, cList *[]*regexp.Regexp) {

	if _, err := os.Stat(cPath); err != nil { fmt.Println("ERROR - Error checking file:", err) }

	file, err := os.Open(cPath)
	if err != nil { fmt.Println("ERROR - opening file:", err); return }
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue // skip empty or commented lines
		}
		re1, _ := regexp.Compile(line)
		*cList = append(*cList, re1)
	}

	if err := scanner.Err(); err != nil { fmt.Println("ERROR - reading file:", err); return }
}