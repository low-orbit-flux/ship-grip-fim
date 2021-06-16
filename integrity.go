

package main

import (
	"crypto/md5"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"sync"
  "time"
	"regexp"
	"strconv"
	"strings"

  //"goji.io"
//"goji.io/pat"
"gopkg.in/mgo.v2"
"gopkg.in/mgo.v2/bson"

)




type SafeFileMap struct {
  v   map[string]string
  mux sync.Mutex
}
type Report struct {
	  Id bson.ObjectId `json:"id" bson:"_id,omitempty"`
		ReportName     string `json:"reportName" bson:"reportName"`
		Host    string `json:"host" bson:"host"`
		Path    string `json:"path" bson:"path"`
		Date    string `json:"date" bson:"date"`
		//Data    map[string]string `json:"data" bson:"data"`
}
type FileHash struct {
	  Id bson.ObjectId `json:"id" bson:"_id,omitempty"`
	  ReportID bson.ObjectId `json:"reportID" bson:"reportID"`
		FilePath    string `json:"filePath" bson:"filePath"`
		Hash    string `json:"hash" bson:"hash"`
}
type DBConnect struct {
    databaseHost string
		database string
		reportCollection string
		fileHashCollection string
}


func sumFile(file string) string {
	f, err := os.Open(file)
	if err != nil {
		log.Print(err)
	}
	defer f.Close()

	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		log.Print(err)
	}

	//fmt.Printf("%x", h.Sum(nil))
	return fmt.Sprintf("%x", h.Sum(nil))
}

func walkFiles(dir string, allFilesList *[]string) {
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		log.Print(err)
	}

	for _, file := range files {
		switch {
		case file.IsDir():
			name := file.Name()
			//fmt.Printf("dir %s\n", name)
			walkFiles(dir+"/"+name, allFilesList) // dir - recursive call
		case file.Mode().IsRegular():
			name := file.Name()
			//sumFile(dir + "/" + name)
			*allFilesList = append(*allFilesList, dir+"/"+name)
			//fmt.Printf("file %v/%v\n", dir, name)
		}
	}
}

func checkFiles(allFilesList *[]string, fileMap *SafeFileMap, wg *sync.WaitGroup ) {

	defer wg.Done()
	for _, file := range *allFilesList {
	  //fmt.Printf("%v\n", file)
		r := sumFile(file)
		//fmt.Printf("\n%v", r)

    fileMap.mux.Lock()
  	fileMap.v[file] = r
  	fileMap.mux.Unlock()
	}


}


func parallelFileCheck( fileMap *SafeFileMap, paraCount int, path string) {

  var wg sync.WaitGroup

	allFilesList := make([]string, 0, 10)

	walkFiles(path, &allFilesList)

  splitIncrement := len(allFilesList) / paraCount
  splitS := 0
  splitE := splitIncrement
  wg.Add(paraCount)
  //fmt.Printf("\narray len: %v\n",len(allFilesList))
  //fmt.Printf("\nsplit increment: %v\n",splitIncrement)
  //fmt.Printf("%v\n",len(allFilesList))
  for i := 0; i < paraCount; i++ {
    aPart := allFilesList[splitS:splitE]
    go checkFiles(&aPart, fileMap, &wg)   // process a slice
    //fmt.Printf("%v %v\n", splitS, splitE)
    splitS = splitE
    splitE += splitIncrement
    // avoid off by one at the end of the array
    if splitE >= len(allFilesList) {
      splitE = len(allFilesList) - 1
    }
    /*
       On the last iteration, make sure we don't leave out some elements
       due to rounding down of split increment.  A couple files kept
       getting left off the end of the list.

       This also fixes going past the end of the array so we don't need the
       check above this but we're keeping it anyway in case we get rid of this
       part.
    */
    if i == paraCount-2 {
      splitE = len(allFilesList)
    }
  }
  wg.Wait()

	for _, file := range allFilesList {
    fileMap.mux.Lock()
		fmt.Printf("%v %v\n", fileMap.v[file], file)
    fileMap.mux.Unlock()
	}
	fmt.Printf("\nNumber of files found: %v", len(allFilesList))
	fmt.Printf("\nNumber of files checked: %v\n", len(fileMap.v))
}


func saveToDB(reportName string, host string, path string, fileMap *SafeFileMap, d DBConnect){
	fileMap.mux.Lock()

  session, err := mgo.Dial(d.databaseHost)
  if err != nil {
    log.Print(err)
  }
  defer session.Close()
  c := session.DB(d.database).C(d.reportCollection)


  t := time.Now()
  timeString := t.Format("2006-01-02_15:04:05") // just format, not hardcoded
	i := bson.NewObjectId()
  report := &Report{
		Id: i,
    ReportName: reportName,
    Host: host,
    Path: path,
    Date: timeString,
  }
  err = c.Insert(report)
  if err != nil {
    log.Print(err)
  }


	session2, err2 := mgo.Dial(d.databaseHost)
  if err2 != nil {
    log.Print(err2)
  }
  defer session2.Close()
  c2 := session2.DB(d.database).C(d.fileHashCollection)

  for k, v := range fileMap.v {
		err2 = c2.Insert(&FileHash{ReportID: i, FilePath: k, Hash: v})
	}

  fileMap.mux.Unlock()
}


func listReports(d DBConnect){

  session, err := mgo.Dial(d.databaseHost)
  if err != nil {
    log.Print(err)
  }
  defer session.Close()
  c := session.DB(d.database).C(d.reportCollection)

  var reportList []Report
	err = c.Find(bson.M{}).Select(bson.M{"_id":1,"reportName":1,"host":1,"path":1,"date":1}).All(&reportList)
  if err != nil {
    log.Print(err)
  }
	for _, report := range reportList {
		fmt.Printf("%v\n", report)
	}

}

func listReportData(reportID string, d DBConnect){

  session, err := mgo.Dial(d.databaseHost)
  if err != nil {
    log.Print(err)
  }
  defer session.Close()
  c := session.DB(d.database).C(d.fileHashCollection)

  var fileHashes []FileHash
	err = c.Find(bson.M{"reportID": bson.ObjectIdHex(reportID)}).All(&fileHashes)
  if err != nil {
    log.Print(err)
  }
	for _, line := range fileHashes {
		fmt.Printf("%v\n", line)
	}

}


func compareReports(reportID1 string, reportID2 string, swapPath1 string, swapPath2 string, d DBConnect){

  /* DB connection 1 */
	session, err := mgo.Dial(d.databaseHost)
  if err != nil {
    log.Print(err)
  }
  defer session.Close()
  c := session.DB(d.database).C(d.fileHashCollection)

	/*
	  - get first report from DB
		  - query based on: report ID 1
		  - save in: fileHashes
	*/
	fmt.Printf("\nLoading first cache...\n\n")
  var fileHashes []FileHash
	err = c.Find(bson.M{"reportID": bson.ObjectIdHex(reportID1)}).All(&fileHashes)
  if err != nil {
    log.Print(err)
  }

  /* DB connection 2	*/
  session2, err2 := mgo.Dial(d.databaseHost)
  if err2 != nil {
    log.Print(err2)
  }
  defer session2.Close()
  c2 := session2.DB(d.database).C(d.fileHashCollection)

  /*
	  - get second report from DB
		  - query based on: report ID 2
		  - save in: fileHashes2
  */
	fmt.Printf("\nLoading second cache...\n\n")
	var fileHashes2 []FileHash
	/*  fileHashesMap does not exist */
  var fileHashesMap2 map[string]string = make(map[string]string)
	err = c2.Find(bson.M{"reportID": bson.ObjectIdHex(reportID2)}).All(&fileHashes2)
  if err != nil {
    log.Print(err)
  }
	/* convert to a map, so we can quickly / easily search */
	fmt.Printf("\nConverting second cache to a map...\n\n")
	for _, i := range fileHashes2 {
		if( swapPath1 == "" || swapPath2 == "") {
			fileHashesMap2[i.FilePath] = i.Hash
		} else {
			/* swap out base path so both filesystem or directory names match */
			hold := i.FilePath
			hold = strings.Replace(hold, swapPath1, swapPath2, 1)
			fileHashesMap2[hold] = i.Hash
		}
	}

  /*
	  - loop over fileHashes (from query 1)
		    - check for a match for each file in fileHashesMap2
  */
	fmt.Println(len(fileHashes))
	fmt.Println(len(fileHashes2))
	fmt.Println(len(fileHashesMap2))
	
	fmt.Printf("\nComparing...\n\n")
  for _, line := range fileHashes {
		/*
		DON'T DO THIS ANY MORE, IT WAS WAY TOO SLOW:
    err = c2.Find(bson.M{"reportID": bson.ObjectIdHex(reportID2), "filePath": line.FilePath }).All(&fh)
    if err2 != nil {
      log.Print(err)
    }
    */

		if val, ok := fileHashesMap2[line.FilePath]; ok {
		  if line.Hash != val {
			  fmt.Printf("ERROR - hashes don't match: %v\n%v\n%v\n", line.FilePath, line.Hash, val)
			}
    }	else {
      fmt.Printf("ERROR - missing file: %v\n", line.FilePath)
		}
  }
	fmt.Printf("\n[Completed]\n\n")
}

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
    d := DBConnect{databaseHost: "localhost",	database: "integrity",	reportCollection: "report",	fileHashCollection: "fileHash"}

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
				    compareReports(os.Args[2], os.Args[3], "", "", d)
			  }
				if(len(os.Args) == 6){
						compareReports(os.Args[2], os.Args[3], os.Args[4], os.Args[5], d)
				}
		default:
			usage()
    }
}
