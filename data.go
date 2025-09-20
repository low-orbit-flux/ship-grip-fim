package main

import (
	//"log"
	"fmt"
)

type reportHeader struct {
	name string 
	time string
	host string
	path string
}


func saveToDB(config configInfo, fileMap *SafeFileMap){
	if config.dataSource == "file" {	
		saveToDBFile(config, fileMap)
	}else {
		fmt.Printf("ERROR - no valid data source specified")
	}

}


func listReports(config configInfo)(string){
	output := ""
	if config.dataSource == "file" {	
		output += listReportsFile(config)
	}else {
		fmt.Printf("ERROR - no valid data source specified")
		output += "ERROR - no valid data source specified"
	}
	return output
}



func listReportData(config configInfo, id1 string){
	if config.dataSource == "file" {	
		listReportDataFile(config, id1)
	}else {
		fmt.Printf("ERROR - no valid data source specified")
	}

}


func reportStat(config configInfo, reportNamePath string)(reportHeader){
	if config.dataSource == "file" {	
		return reportStatFile(config, reportNamePath)
	}else {
		fmt.Printf("ERROR - no valid data source specified")
		return reportHeader{}  // return empty
	}
}


func compareReportsData(config configInfo, oldReportName string, newReportName string, oldReport map[string]string, newReport map[string]string, oldHeader reportHeader, newHeader reportHeader){
	if( config.dataSource == "file" ) {
		compareReportsDataFile(config, oldReportName, newReportName, oldReport, newReport, oldHeader, newHeader)
	} else {
		fmt.Printf("ERROR - no valid data source specified")
	}
}


func saveCompare(config configInfo, compareReportName string, oldHeader reportHeader, newHeader reportHeader, cr compareReport){
	if config.dataSource == "file" {	
		saveCompareFile(config, compareReportName, oldHeader, newHeader, cr)
	}else {
		fmt.Printf("ERROR - no valid data source specified")
	}
}
