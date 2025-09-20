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
	test string
}

func usage() {
	usageString := `
Usage:
    ship-grip-fim scan 
    ship-grip-fim list
    ship-grip-fim data <ID>
    ship-grip-fim compare <ID> <ID>
	ship-grip-fim --removeBasePath=true compare default_adhoc_report_2025-09-15_23:09:49 default_adhoc_report_2025-09-15_23:10:28

    scan - This will take a checksum of every file in the specified directory.
           This is done for all files recursively.  The results are written
           to the database.

    list - This will list out all reports in the database.

    data - This will dump all of the data entries from a given report.  The
           data consists of path / checksum pairs.

    compare - This will compare the checksum for each file in two different
              reports.  If a checksum has changed, it will be shown.  If a
              file is missing, it will be shown.  The ID for the older report
              is listed first, then the ID for the newer report.  
			  
			  If the param "--removeBasePath true" is used the base path will be removed for all files. 
			  This helps if you file system was moved or mounted somewhere else and you 
			  still need to check if all files match. 

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

    flag.Usage = usage
	flag.Parse()                 // args are pointers because this function needs it
	positional := flag.Args()
	action := ""
	if len(positional) >= 1 {
	    action = positional[0] // scan, list, data, compare, serve, connect
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
	if *ignorePathNoWalkConfig_ptr != "----" { config.ignorePathNoWalkConfig       = *ignorePathNoWalkConfig_ptr }



    loadConfigsOther(config.ignorePathConfig, &config.ignorePath)            // load other configs ( exclude files)
	loadConfigsOther(config.ignorePathNoWalkConfig, &config.ignorePathNoWalk)// load other configs ( exclude files)



    switch action {
		case "scan":
			if _, err := os.Stat(config.path); err != nil {
				if os.IsNotExist(err) { fmt.Println("ERROR - Directory does not exist")
				} else { fmt.Println("ERROR - ", err) }
			} else {
                fileMap := SafeFileMap{v: make(map[string]string)}
		    	fmt.Printf("ParaCount: %v\n\n",config.paraCount)
                parallelFileCheck(config, &fileMap )
		    	fmt.Printf("test %s, %s, %s, %s", config.reportName, config.host, config.path, config.reportDir)
		        saveToDB(config, &fileMap)
			}
		case "list":	
			listReports(config)
			
		case "data":
			listReportData(config, id1)
			
		case "compare":
    	    compareReports(config, id1, id2)  
	/*
		case "server":
		    startServer()
		default:
			usage()
    */
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