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

	_, e1 := os.Stat(reportDir)
    if os.IsNotExist(e1) {
    	err := os.Mkdir(reportDir, 0755)
        if err != nil {
            fmt.Print("\n\n\n\nERROR - Can't create report dir.\n\n\n\n")
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
	if _, err = f.WriteString(reportName + "," + timeString + "," + host + "," + path + "\n"); err != nil {
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


func listReportsFile()(string){
	output := ""
    f, err := ioutil.ReadDir(reportDir)
    if err != nil {
        fmt.Println("\n\n\n============================================\n")
        fmt.Println("Empty, no reports or other error, see below.\n")
        fmt.Println("============================================\n\n\n")
        fmt.Println(err)
        fmt.Println("\n--------------------------------------------\n\n\n")
    }
    for _, i := range f {
        fmt.Println(i.Name())
		output += i.Name() + "\n"
    } 
	return output
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

func reportStatFile(reportName string)(reportHeader){
    // could have just returned this info from compareReportsDataFile() but 
    // nice to have a dedicated function for other purposes

    f, err := os.Open(reportDir + "/" + reportName)
    if err != nil {
        panic(err)
    }
    defer f.Close()
    s := bufio.NewScanner(f)
    s.Scan() 
    h := strings.SplitN(s.Text(),",",4)  // watch for commas in file names
    rh := reportHeader{}
    if len(h) >= 4 {
        rh = reportHeader{ name:h[0], time:h[1], host:h[2], path:h[3]}
    }else {
        fmt.Println("Error - header not parsed")
    }
    if err := s.Err(); err != nil {
        panic(err)
    }
    return rh
}

func compareReportsDataFile(oldReportName string, newReportName string, oldReport map[string]string, newReport map[string]string, oldHeader reportHeader, newHeader reportHeader, removeBasePath string){
  

	fmt.Printf("\nLoading first cache...\n\n")

    f, err := os.Open(reportDir + "/" + oldReportName)
    if err != nil {
        panic(err)
    }
    defer f.Close()
    s := bufio.NewScanner(f)
    s.Scan() // remove header, that we already have .....
    for s.Scan() {
        lines1 := strings.SplitAfterN(s.Text(),",",2)
		if len(lines1) == 2 {
            //fmt.Println(lines1[0] + lines1[1])
			oldReport[lines1[1]] = lines1[0] 
		} else {
            fmt.Println("ERROR - line split in to more or less than 2 cols")
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
    s2.Scan() // remove header, that we already have .....
    for s2.Scan() {
        lines1 := strings.SplitAfterN(s2.Text(),",",2)
		if len(lines1) == 2 {
            //fmt.Println(lines1[0] + lines1[1])
			newReport[lines1[1]] = lines1[0]
		} else {
            fmt.Println("ERROR - line split in to more or less than 2 cols")
        }
    }
    if err := s2.Err(); err != nil {
        panic(err)
    }

    if removeBasePath == "yes" {
        keys1 := make([]string, 0, len(oldReport))
        for k := range oldReport {
            keys1 = append(keys1, k)
        }
        keys2 := make([]string, 0, len(newReport))
        for k := range newReport {
            keys2 = append(keys2, k)
        }
        for _, k := range keys1 {
            v := oldReport[k]
            k2 := strings.ReplaceAll(k, oldHeader.path, "")
            delete(oldReport, k) // do this first (edge case), incase a base path didn't exist and couldn't be removed the keys could be the same ( should never happen though )
            oldReport[k2] = v
        }
        for _, k := range keys2 {
            v := newReport[k]
            k2 := strings.ReplaceAll(k, newHeader.path, "")
            delete(newReport, k) // do this first (edge case), incase a base path didn't exist and couldn't be removed the keys could be the same ( should never happen though )
            newReport[k2] = v
        }
    }
}






func saveCompareFile(reportName string, oldHeader reportHeader, newHeader reportHeader, cr compareReport){
	_, e1 := os.Stat(reportDir)
    if os.IsNotExist(e1) {
    	err := os.Mkdir(reportDir, 0755)
        if err != nil {
            fmt.Print("\n\n\n\nERROR - Can't create report dir.\n\n\n\n")
            panic(err)
        }
	}

    t := time.Now()
    timeString := t.Format("2006-01-02_15:04:05") // just format, not hardcoded

	f, err := os.OpenFile(reportDir + "/" + reportName + "__" + timeString, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if _, err = f.WriteString("Old Header: " + oldHeader.name + "," + oldHeader.time + "," + oldHeader.host + "," + oldHeader.path + "\n"); err != nil {
		panic(err)
	}
    if _, err = f.WriteString("New Header: " + newHeader.name + "," + newHeader.time + "," + newHeader.host + "," + newHeader.path + "\n"); err != nil {
		panic(err)
	}
    
	f2, err := os.OpenFile(reportDir + "/" + reportName + "__" + timeString, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		panic(err)
	}
	defer f2.Close()



    for k, v := range cr.newFiles {
        fmt.Println("NEW - " + k + " - " + v)
        if _, err = f2.WriteString("NEW - " + k + " - " + v + "\n"); err != nil {panic(err)}
    }
    for k, v := range cr.missingFiles {
        fmt.Println("MISSING - " + k + " - " + v)
        if _, err = f2.WriteString("MISSING - " + k + " - " + v + "\n"); err != nil {
			panic(err)
		}
    }
    for _, v := range cr.changedFiles {
        fmt.Println("CHANGED - " + v.path + " - " + v.oldHash + " ==> " + v.newHash )
        if _, err = f2.WriteString("CHANGED - " + v.path + " - " + v.oldHash + " ==> " + v.newHash + "\n"); err != nil {
			panic(err)
		}
    }
    for _, v := range cr.movedFiles {
        fmt.Println("MOVED - " + v.oldPath + " ==> " + v.newPath + " - " + v.hash )
        if _, err = f2.WriteString("MOVED - " + v.oldPath + " ==> " + v.newPath + " - " + v.hash + "\n"); err != nil {
			panic(err)
		}
    }





}