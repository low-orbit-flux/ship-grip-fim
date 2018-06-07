/*
   - comparing reports
   - config files and hardcoded values
   - some files don't get md5 sums
   - should variables stay global?
   - database and table are hardcoded
	 - do something about trailing slashes when paths ar concatenated
   - figure out the newest and second newest reports
	 - daemon the schedules runs
	     - schedule runs, save reports
	     - have alerts ( email, etc.)
	 - GUI
	 - daemon status viewer
	 - report viewer
*/

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

  //"goji.io"
//"goji.io/pat"
"gopkg.in/mgo.v2"
"gopkg.in/mgo.v2/bson"

)

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

func checkFiles(allFilesList *[]string, fileMap *SafeFileMap ) {

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

type SafeFileMap struct {
  v   map[string]string
  mux sync.Mutex
}

var wg sync.WaitGroup // should this stay global ????

func parallelFileCheck( fileMap *SafeFileMap, paraCount int, path string) {

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
    go checkFiles(&aPart, fileMap)   // process a slice
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

  //fmt.Printf("\n\n")

	for _, file := range allFilesList {
    fileMap.mux.Lock()
		fmt.Printf("%v %v\n", fileMap.v[file], file)
    fileMap.mux.Unlock()
	}
	fmt.Printf("\nNumber of files found: %v", len(allFilesList))
	fmt.Printf("\nNumber of files checked: %v\n", len(fileMap.v))
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



//func saveToDB(fileMap *SafeFileMap, sdf string){
func saveToDB(reportName string, host string, path string, fileMap *SafeFileMap){
	fileMap.mux.Lock()

  session, err := mgo.Dial("localhost")
  if err != nil {
    log.Print(err)
  }
  defer session.Close()
  c := session.DB("integrity").C("report")


  t := time.Now()
  timeString := t.Format("2006-01-02_15:04:05")
	i := bson.NewObjectId()
  report := &Report{
		Id: i,
    ReportName: reportName,
    Host: host,
    Path: path,
    Date: timeString,
    //Data: fileMap.v,
  }
  err = c.Insert(report)
  if err != nil {
    log.Print(err)
  }


	session2, err2 := mgo.Dial("localhost")
  if err2 != nil {
    log.Print(err2)
  }
  defer session2.Close()
  c2 := session2.DB("integrity").C("fileHash")

  for k, v := range fileMap.v {
		err2 = c2.Insert(&FileHash{ReportID: i, FilePath: k, Hash: v})
	}

  fileMap.mux.Unlock()
}


func listReports(){

  session, err := mgo.Dial("localhost")
  if err != nil {
    log.Print(err)
  }
  defer session.Close()
  c := session.DB("integrity").C("report")

  var reportList []Report
  //err = c.Find(bson.M{}).Select(bson.M{"data":0}).All(&reportList)
	err = c.Find(bson.M{}).Select(bson.M{"_id":1,"reportName":1,"host":1,"path":1,"date":1}).All(&reportList)
  if err != nil {
    log.Print(err)
  }
	for _, report := range reportList {
		fmt.Printf("%v\n", report)
	}

}

func listReportData(reportID string){

  session, err := mgo.Dial("localhost")
  if err != nil {
    log.Print(err)
  }
  defer session.Close()
  c := session.DB("integrity").C("fileHash")

  var fileHashes []FileHash
	err = c.Find(bson.M{"reportID": bson.ObjectIdHex(reportID)}).All(&fileHashes)
  if err != nil {
    log.Print(err)
  }
	for _, line := range fileHashes {
		fmt.Printf("%v\n", line)
	}

}


func compareReports(reportID1 string, reportID2 string){
	session, err := mgo.Dial("localhost")
  if err != nil {
    log.Print(err)
  }
  defer session.Close()
  c := session.DB("integrity").C("fileHash")

  var fileHashes []FileHash
	err = c.Find(bson.M{"reportID": bson.ObjectIdHex(reportID)}).All(&fileHashes)
  if err != nil {
    log.Print(err)
  }
	for _, line := range fileHashes {
		fmt.Printf("%v\n", line)
	}

}


func main() {

//db.integrity.find({ "data./home/user1/test/test6": { $exists : true}},{"data./home/user1/test/test6":1})
//db.integrity.find({ "data./home/user1/test/test9\.php": { $exists : true}},{"data./home/user1/test/test9\.php":1})

    switch os.Args[1] {
		case "scan":
        fileMap := SafeFileMap{v: make(map[string]string)}
        parallelFileCheck(&fileMap, 8, os.Args[2])
        saveToDB("adhoc report 1", "duck-puppy", "/storage1", &fileMap)
			case "list":
			/*
			 - need to compare two reports
			          - parallelize this
			*/
			  listReports()
			case "data":
				listReportData(os.Args[2])
			case "compare":
				compareReports()
		default:
			fmt.Printf("nothing selected ...\n")
			fmt.Printf("Usage:  scan <path>, list, data <ID>, compare <ID> <ID>\n")
    }
}
