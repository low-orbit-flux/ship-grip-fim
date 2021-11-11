package main


func compareReportsDataFile(reportID1 string, reportID2 string)([]FileHash,[]FileHash){
  
	d := DBConnect{databaseHost: "localhost",	database: "integrity",	reportCollection: "report",	fileHashCollection: "fileHash"}
	
	var fileHashes []FileHash  
    var fileHashes2 []FileHash

	fmt.Printf("\nLoading first cache...\n\n")
    data1, err := ioutil.ReadFile(reportDir + "/" + reportID1)
    if err != nil {
          log.Fatal(err)
    }
	lines1 := strings.Split(data1,"\n")
    for i, v := range lines1 {
		lines1 := strings.Split(v,":")
		//fileHashes[0] = ????
		//
		//   Load everything to a hash map for both DBS 
		//   Remove the part in logic.go that converts these
		
	}


	fmt.Printf("\nLoading second cache...\n\n")

	//
	//  do the same for the second cache
	//

	return fileHashes, fileHashes2
}