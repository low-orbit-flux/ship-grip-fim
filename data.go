package main

import (
	"fmt"
	"strings"
)

type reportHeader struct {
	name string
	time string
	host string
	path string
}

var errNoDataSource = fmt.Errorf("no valid data source specified")

// reportTimeFormat is the timestamp suffix appended to report file names.
// Dashes are used in the time part because ":" is not a legal filename
// character on Windows.  parseReportTimestamp still accepts the older
// "15:04:05" form so existing reports keep working.
const reportTimeFormat = "2006-01-02_15-04-05"

// validReportID reports whether id is safe to use as a report file name.
// Report IDs arrive from the network (agent protocol, web GUI, remote agents
// during sync) so they must never be able to escape the report directory.
func validReportID(id string) bool {
	if id == "" || id == "." || id == ".." {
		return false
	}
	if strings.ContainsAny(id, "/\\\x00 \t\r\n") {
		return false
	}
	return true
}

func saveToDB(config configInfo, fileMap *SafeFileMap) error {
	if config.dataSource == "file" {
		return saveToDBFile(config, fileMap)
	}
	return errNoDataSource
}

func listReports(config configInfo) string {
	if config.dataSource == "file" {
		return listReportsFile(config)
	}
	return "ERROR - " + errNoDataSource.Error() + "\n"
}

func listReportData(config configInfo, id1 string) error {
	if config.dataSource == "file" {
		return listReportDataFile(config, id1)
	}
	return errNoDataSource
}

// listReportDataString returns the report contents as a string (used by agent).
func listReportDataString(config configInfo, id1 string) string {
	if config.dataSource == "file" {
		return listReportDataStringFile(config, id1)
	}
	return "ERROR - " + errNoDataSource.Error() + "\n"
}

func reportStat(config configInfo, reportNamePath string) (reportHeader, error) {
	if config.dataSource == "file" {
		return reportStatFile(config, reportNamePath)
	}
	return reportHeader{}, errNoDataSource
}

func compareReportsData(config configInfo, oldReportName string, newReportName string, oldReport map[string]string, newReport map[string]string, oldHeader reportHeader, newHeader reportHeader) error {
	if config.dataSource == "file" {
		return compareReportsDataFile(config, oldReportName, newReportName, oldReport, newReport, oldHeader, newHeader)
	}
	return errNoDataSource
}

func saveCompare(config configInfo, compareReportName string, oldHeader reportHeader, newHeader reportHeader, cr compareReport) error {
	if config.dataSource == "file" {
		return saveCompareFile(config, compareReportName, oldHeader, newHeader, cr)
	}
	return errNoDataSource
}
