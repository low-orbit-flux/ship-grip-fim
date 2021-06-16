package main


func compareReportsDataMongo(reportID1 string, reportID2 string)([]FileHash,[]FileHash){
  
	d := DBConnect{databaseHost: "localhost",	database: "integrity",	reportCollection: "report",	fileHashCollection: "fileHash"}

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

    err = c2.Find(bson.M{"reportID": bson.ObjectIdHex(reportID2)}).All(&fileHashes2)
	if err != nil {
	  log.Print(err)
	}

	return fileHashes, fileHashes2
}