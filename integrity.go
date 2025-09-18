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
	//"bufio"
	//"io"
	"io/ioutil"
	"log"	
	"os"
	"flag"
	"regexp"
	"strconv"
)



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

/*
    Global hardcoded settings, these will be used if there are no args and nothing
		is found in the config file.
*/
var reportDir = "~/integirty_reports"
var dataSource = "file"  

func main() {

    /*
        Hardcoded settings, these will be used if there are no args and nothing
	    	is found in the config file.
    */
    reportName := "default_adhoc_report"
    host := "duck-puppy"
  	path := "/storage1"
    paraCount := 8
	removeBasePath := false


    // named params, "----" is default and won't override config file 
	reportDir_ptr := flag.String("reportDir", "----", "report dir")
    dataSource_ptr := flag.String("dataSource", "----", "data source type")
    reportName_ptr := flag.String("reportName", "----", "report name")
    host_ptr := flag.String("host", "----", "host")
    path_ptr := flag.String("path", "----", "path")
    paraCount_ptr := flag.String("paraCount", "----", "parallel instances ( CPU cores to use )")   
	removeBasePath_ptr := flag.String("removeBasePath", "----", "remove base path")  


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




//     - check if config file path was overridden


    // Read settings from config file, these may be overridden by any commandline args
 	configData, err := ioutil.ReadFile("integrity.conf")
    if err != nil {
        fmt.Println( "ERROR - can't read config file, using defaults")
    }
    cf := string(configData)
    re1, err := regexp.Compile(`(?m)^(reportName)="(.*)"`)
    if r := re1.FindAllStringSubmatch(cf, -1); r != nil { reportName = r[0][2] }
	re1, err = regexp.Compile(`(?m)^(host)="(.*)"`)
    if r := re1.FindAllStringSubmatch(cf, -1); r != nil { host = r[0][2] }
	re1, err = regexp.Compile(`(?m)^(path)="(.*)"`)
    if r := re1.FindAllStringSubmatch(cf, -1); r != nil { path = r[0][2]	}
	re1, err = regexp.Compile(`(?m)^(reportDir)="(.*)"`)
    if r := re1.FindAllStringSubmatch(cf, -1); r != nil { reportDir = r[0][2] }
	re1, err = regexp.Compile(`(?m)^(dataSource)="(.*)"`)
	if r := re1.FindAllStringSubmatch(cf, -1); r != nil { dataSource = r[0][2] }
	re1, err = regexp.Compile(`(?m)^(paraCount)="(.*)"`)
	if r := re1.FindAllStringSubmatch(cf, -1); r != nil { paraCount, err = strconv.Atoi(r[0][2]) }
	re1, err = regexp.Compile(`(?m)^(removeBasePath)="(.*)"`)
	if r := re1.FindAllStringSubmatch(cf, -1); r != nil { removeBasePath, _ = strconv.ParseBool(r[0][2]) }

    /*
		- CLI args override config file options here
		- only if they don't have the default value ("----"), meaning they were actually set
		- also need to be de-referenced and converted
    */
	if *reportDir_ptr != "----"  { reportDir  = *reportDir_ptr }
	if *dataSource_ptr != "----" { dataSource = *dataSource_ptr }
	if *reportName_ptr != "----" { reportName = *reportName_ptr }
	if *host_ptr != "----"       { host       = *host_ptr }
	if *path_ptr != "----"       { path       = *path_ptr }
	if *paraCount_ptr != "----"  { paraCount, _ = strconv.Atoi(*paraCount_ptr) }
    if *removeBasePath_ptr != "----" { removeBasePath, _ = strconv.ParseBool(*removeBasePath_ptr) }
fmt.Println("debug")
fmt.Println(reportDir)
fmt.Println(reportDir_ptr)


    switch action {
		case "scan":
			if _, err := os.Stat(path); err != nil {
				if os.IsNotExist(err) { fmt.Println("ERROR - Directory does not exist")
				} else { fmt.Println("ERROR - ", err) }
			} else {
                fileMap := SafeFileMap{v: make(map[string]string)}
		    	fmt.Printf("ParaCount: %v\n\n",paraCount)
                parallelFileCheck(&fileMap, paraCount, path)
		    	fmt.Printf("test %s, %s, %s, %s",reportName, host, path, reportDir)
		        saveToDB(reportName, host, path, &fileMap)
			}
		case "list":	
			listReports()
			
		case "data":
			listReportData(id1)
			
		case "compare":
    	    compareReports(id1, id2, removeBasePath)  
	/*
		case "server":
		    startServer()
		default:
			usage()
    */
    }
}
