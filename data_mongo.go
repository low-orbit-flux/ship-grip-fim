package main


// Old and probably not needed for anything anymore 
/*
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
*/


/*



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




func compareReportsDataMongo(reportID1 string, reportID2 string)([]FileHash,[]FileHash){
  
	d := DBConnect{databaseHost: "localhost",	database: "integrity",	reportCollection: "report",	fileHashCollection: "fileHash"}

	// DB connection 1 
	session, err := mgo.Dial(d.databaseHost)
	if err != nil {
	    log.Print(err)
	}
	defer session.Close()
	c := session.DB(d.database).C(d.fileHashCollection)
  
	//	- get first report from DB
	//	- query based on: report ID 1
	//	- save in: fileHashes
	fmt.Printf("\nLoading first cache...\n\n")
	var fileHashes []FileHash
	err = c.Find(bson.M{"reportID": bson.ObjectIdHex(reportID1)}).All(&fileHashes)
	if err != nil {
	    log.Print(err)
	}
  
	// DB connection 2	
	session2, err2 := mgo.Dial(d.databaseHost)
	if err2 != nil {
	    log.Print(err2)
	}
	defer session2.Close()
	c2 := session2.DB(d.database).C(d.fileHashCollection)
  
	//	- get second report from DB
	//	- query based on: report ID 2
	//	- save in: fileHashes2
    fmt.Printf("\nLoading second cache...\n\n")
    var fileHashes2 []FileHash

    err = c2.Find(bson.M{"reportID": bson.ObjectIdHex(reportID2)}).All(&fileHashes2)
	if err != nil {
	    log.Print(err)
	}

	return fileHashes, fileHashes2
}


func saveToDBMongo(reportName string, host string, path string, fileMap *SafeFileMap){
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


*/