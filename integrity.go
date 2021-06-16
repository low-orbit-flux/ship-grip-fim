package main

import (
	"fmt"
	"io/ioutil"
	"log"	
	"os"
	"regexp"
	"strconv"
)



func usage() {
	usageString := `
Usage:
    ship-grip-fim scan <path>
    ship-grip-fim list
    ship-grip-fim data <ID>
    ship-grip-fim compare <ID> <ID>
    ship-grip-fim compare <ID> <ID> <path1> <path2>

    scan - This will take a checksum of every file in the specified directory.
           This is done for all files recursively.  The results are written
           to the database.

    list - This will list out all reports in the database.

    data - This will dump all of the data entries from a given report.  The
           data consists of path / checksum pairs.

    compare - This will compare the checksum for each file in two different
              reports.  If a checksum has changed, it will be shown.  If a
              file is missing, it will be shown.  The ID for the older report
              is listed first, then the ID for the newer report.  If two paths
              are passed, they will be used to change the prefix for all of the
              paths form the newer report; path2 replaces path1.



	`
	fmt.Printf(usageString)
	log.Fatal("Exiting ...")
}


func main() {



/*
    Hardcoded settings, these will be used if there are no args and nothing
		is found in the config file.
*/

    reportName := "default adhoc report"
    host := "duck-puppy"
  	path := "/storage1"
    paraCount := 8
	dataSource = "file"  // default

/*
    Read settings from config file
		- these may be overridden by any commandline args
*/
 		configData, err := ioutil.ReadFile("integrity.conf")
    if err != nil {
        log.Fatal(err)
    }
    cf := string(configData)

    re1, err := regexp.Compile(`(reportName)="(.*)"`)
    r := re1.FindAllStringSubmatch(cf, -1)
    reportName = r[0][2]

		re1, err = regexp.Compile(`(host)="(.*)"`)
    r = re1.FindAllStringSubmatch(cf, -1)
    host = r[0][2]

		re1, err = regexp.Compile(`(path)="(.*)"`)
    r = re1.FindAllStringSubmatch(cf, -1)
    path = r[0][2]

		re1, err = regexp.Compile(`(paraCount)="(.*)"`)
		r = re1.FindAllStringSubmatch(cf, -1)
		paraCount, err = strconv.Atoi(r[0][2])

		re1, err = regexp.Compile(`(databaseHost)="(.*)"`)
		r = re1.FindAllStringSubmatch(cf, -1)
		d.databaseHost = r[0][2]

		re1, err = regexp.Compile(`(database)="(.*)"`)
		r = re1.FindAllStringSubmatch(cf, -1)
		d.database = r[0][2]

		re1, err = regexp.Compile(`(reportCollection)="(.*)"`)
		r = re1.FindAllStringSubmatch(cf, -1)
		d.reportCollection = r[0][2]

		re1, err = regexp.Compile(`(fileHashCollection)="(.*)"`)
		r = re1.FindAllStringSubmatch(cf, -1)
		d.fileHashCollection = r[0][2]

    if(len(os.Args) < 2){
	    usage()
    }

    switch os.Args[1] {
		case "scan":
		if(len(os.Args) >= 3){
			path = os.Args[2] // override this
		}

        fileMap := SafeFileMap{v: make(map[string]string)}
				fmt.Printf("ParaCount: %v",paraCount)
        parallelFileCheck(&fileMap, paraCount, path)
        saveToDB(reportName, host, path, &fileMap, d)
			case "list":
			/*
			 - need to compare two reports
			          - parallelize this
			*/
			  listReports(d)
			case "data":
				if(len(os.Args) != 3){
					usage()
				}
				listReportData(os.Args[2], d)
			case "compare":
				if(len(os.Args) != 4 && len(os.Args) != 6){
					usage()
				}
				if(len(os.Args) == 4){
				    compareReports(os.Args[2], os.Args[3], "", "")
			  }
				if(len(os.Args) == 6){
						compareReports(os.Args[2], os.Args[3], os.Args[4], os.Args[5])
				}
		default:
			usage()
    }
}
