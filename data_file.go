package main

import (
	"time"
	"os"
	"fmt"
	"io/ioutil"
	"bufio"
	"strings"
)

func saveToDBFile(reportName string, host string, path string, fileMap *SafeFileMap){
	fileMap.mux.Lock()

	fileinfo, e1 := os.Stat(reportDir)
	fmt.Print(fileinfo)
    if os.IsNotExist(e1) {
    	err := os.Mkdir(reportDir, 0755)
        if err != nil {
            panic(err)
        }
	}

    t := time.Now()
    timeString := t.Format("2006-01-02_15:04:05") // just format, not hardcoded
	fmt.Print(timeString)
    
	f, err := os.OpenFile(reportDir + "/" + reportName + "_" + timeString, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if _, err = f.WriteString(reportName + "," + host + "," + path + "," + timeString + "\n"); err != nil {
		panic(err)
	}
	f2, err := os.OpenFile(reportDir + "/" + reportName + "_" + timeString, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		panic(err)
	}
	defer f2.Close()
	for k, v := range fileMap.v {
		if _, err = f2.WriteString(v + "," + k + "\n"); err != nil {
			panic(err)
		}
	}

	fileMap.mux.Unlock()
}


func listReportsFile(){
    f, err := ioutil.ReadDir(reportDir)
    if err != nil {
        panic(err)
    }
    for _, i := range f {
        fmt.Println(i.Name())
    } 
}



func listReportDataFile(reportName string){

    f, err := os.Open(reportDir + "/" + reportName)
    if err != nil {
        panic(err)
    }
    defer f.Close()
    s := bufio.NewScanner(f)
    for s.Scan() {
        fmt.Println(s.Text())
    }
    if err := s.Err(); err != nil {
        panic(err)
    }
}



func compareReportsDataFile(oldReportName string, newReportName string, oldReport map[string]string, newReport map[string]string){
  

	fmt.Printf("\nLoading first cache...\n\n")

    f, err := os.Open(reportDir + "/" + oldReportName)
    if err != nil {
        panic(err)
    }
    defer f.Close()
    s := bufio.NewScanner(f)
    for s.Scan() {
        fmt.Println(s.Text())
        lines1 := strings.SplitAfter(s.Text(),",")
		if len(lines1) == 2 {
            fmt.Println(lines1[0] + "--" + lines1[1])
			oldReport[lines1[1]] = lines1[0] 
		}
    }
    if err := s.Err(); err != nil {
        panic(err)
    }



	fmt.Printf("\nLoading second cache...\n\n")


    f2, err := os.Open(reportDir + "/" + newReportName)
    if err != nil {
        panic(err)
    }
    defer f2.Close()
    s2 := bufio.NewScanner(f2)
    for s2.Scan() {
        fmt.Println(s2.Text())
        lines1 := strings.SplitAfter(s2.Text(),",")
		if len(lines1) == 2 {
            fmt.Println(lines1[0] + "--" + lines1[1])
			newReport[lines1[1]] = lines1[0]
		} 
    }
    if err := s2.Err(); err != nil {
        panic(err)
    }
}
