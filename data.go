package main
/*
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
*/


type DBConnect struct {
	databaseHost string
		database string
		reportCollection string
		fileHashCollection string
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




func compareReportsData(reportID1 string, reportID2 string)([]FileHash,[]FileHash){
  
	d := DBConnect{databaseHost: "localhost",	database: "integrity",	reportCollection: "report",	fileHashCollection: "fileHash"}

	if( dataSource == "mongo" ) {
		fileHashes, fileHashes2 = compareReportsDataMongo()
	} else {
		fileHashes, fileHashes2 = compareReportsDataFile()
	}

	

	return fileHashes, fileHashes2
}
  