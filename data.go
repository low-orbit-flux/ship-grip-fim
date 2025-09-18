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

type reportHeader struct {
	name string 
	time string
	host string
	path string
}


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


func reportStat(reportName string)(reportHeader){
	if dataSource == "file" {	
		return reportStatFile(reportName)
	}else {
		fmt.Printf("ERROR - no valid data source specified")
		return reportHeader{}  // return empty
	}
}


func compareReportsData(oldReportName string, newReportName string, oldReport map[string]string, newReport map[string]string, oldHeader reportHeader, newHeader reportHeader, removeBasePath bool){
	if( dataSource == "file" ) {
		compareReportsDataFile(oldReportName, newReportName, oldReport, newReport, oldHeader, newHeader, removeBasePath)
	} else {
		fmt.Printf("ERROR - no valid data source specified")
	}
}


func saveCompare(reportName string, oldHeader reportHeader, newHeader reportHeader, cr compareReport){
	if dataSource == "file" {	
		saveCompareFile(reportName, oldHeader, newHeader, cr)
	}else {
		fmt.Printf("ERROR - no valid data source specified")
	}
}
