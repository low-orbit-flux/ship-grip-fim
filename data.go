package main

import (
	//"log"
	"fmt"
)

/*


type DBConnect struct {
	databaseHost string
	database string
	reportCollection string
	fileHashCollection string
}


*/


func saveToDB(reportName string, host string, path string, fileMap *SafeFileMap){
	if dataSource == "file" {	
		saveToDBFile(reportName, host, path, fileMap)
	}else {
		fmt.Printf("ERROR - no valid data source specified")
	}

}


func listReports()(string){
	output := ""
	if dataSource == "file" {	
		output += listReportsFile()
	}else {
		fmt.Printf("ERROR - no valid data source specified")
		output += "ERROR - no valid data source specified"
	}
	return output
}



func listReportData(reportName string){
	if dataSource == "file" {	
		listReportDataFile(reportName)
	}else {
		fmt.Printf("ERROR - no valid data source specified")
	}

}



func compareReportsData(oldReportName string, newReportName string, oldReport map[string]string, newReport map[string]string){
	if( dataSource == "file" ) {
		compareReportsDataFile(oldReportName, newReportName, oldReport, newReport)
	} else {
		fmt.Printf("ERROR - no valid data source specified")
	}
}
