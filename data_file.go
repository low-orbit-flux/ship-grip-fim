package main


func compareReportsDataMongo(reportID1 string, reportID2 string)([]FileHash,[]FileHash){
  
	d := DBConnect{databaseHost: "localhost",	database: "integrity",	reportCollection: "report",	fileHashCollection: "fileHash"}
	
	var fileHashes []FileHash  
    var fileHashes2 []FileHash

	fmt.Printf("\nLoading first cache...\n\n")
    // Read from a file here <==============================
	fmt.Printf("\nLoading second cache...\n\n")
	// Read from a file here <==============================

	return fileHashes, fileHashes2
}