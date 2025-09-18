package main

import (
	"crypto/md5"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"sync"
	//"regexp"
	//"strconv"
	//"strings"
  //"goji.io"
//"goji.io/pat"
)



// thread safe type used to hold hash of files after scan
type SafeFileMap struct {
	v   map[string]string
	mux sync.Mutex
  }
  
func sumFile(file string) string {
	  f, err := os.Open(file)
	  if err != nil {
		  log.Print(err)
	  }
	  defer f.Close()
  
	  h := md5.New()
	  if _, err := io.Copy(h, f); err != nil {
		  log.Print(err)
	  }
  
	  //fmt.Printf("%x", h.Sum(nil))
	  return fmt.Sprintf("%x", h.Sum(nil))
  }
  
  func walkFiles(dir string, allFilesList *[]string) {
	  files, err := ioutil.ReadDir(dir)
	  if err != nil {
		  log.Print(err)
	  }
  
	  for _, file := range files {
		  switch {
		  case file.IsDir():
			  name := file.Name()
			  //fmt.Printf("dir %s\n", name)
			  walkFiles(dir+"/"+name, allFilesList) // dir - recursive call
		  case file.Mode().IsRegular():
			  name := file.Name()
			  //sumFile(dir + "/" + name)
			  *allFilesList = append(*allFilesList, dir+"/"+name)
			  //fmt.Printf("file %v/%v\n", dir, name)
		  }
	  }
  }
  
  func checkFiles(allFilesList *[]string, fileMap *SafeFileMap, wg *sync.WaitGroup ) {
  
	  defer wg.Done()
	  for _, file := range *allFilesList {
		//fmt.Printf("%v\n", file)
		  r := sumFile(file)
		  //fmt.Printf("\n%v", r)
  
	  fileMap.mux.Lock()
		fileMap.v[file] = r
		fileMap.mux.Unlock()
	  }
  
  
  }

  
func parallelFileCheck( fileMap *SafeFileMap, paraCount int, path string) {

	var wg sync.WaitGroup
  
	allFilesList := make([]string, 0, 10)
  
	walkFiles(path, &allFilesList)
  
	splitIncrement := len(allFilesList) / paraCount
	splitS := 0
	splitE := splitIncrement
	wg.Add(paraCount)
	//fmt.Printf("\narray len: %v\n",len(allFilesList))
	//fmt.Printf("\nsplit increment: %v\n",splitIncrement)
	//fmt.Printf("%v\n",len(allFilesList))
	for i := 0; i < paraCount; i++ {
	    aPart := allFilesList[splitS:splitE]
	    go checkFiles(&aPart, fileMap, &wg)   // process a slice
	    //fmt.Printf("%v %v\n", splitS, splitE)
	    splitS = splitE
	    splitE += splitIncrement
	    // avoid off by one at the end of the array
	    if splitE >= len(allFilesList) {
	    	splitE = len(allFilesList) - 1
	    }
	  
    	//	 On the last iteration, make sure we don't leave out some elements
	    //	 due to rounding down of split increment.  A couple files kept
	    //	 getting left off the end of the list.
        //
	    //	 This also fixes going past the end of the array so we don't need the
	    //	 check above this but we're keeping it anyway in case we get rid of this
	    //	 part.
	  
	    if i == paraCount-2 {
	    	splitE = len(allFilesList)
	    }
	}
	wg.Wait()
  
	for _, file := range allFilesList {
	    fileMap.mux.Lock()
		fmt.Printf("%v %v\n", fileMap.v[file], file)
	    fileMap.mux.Unlock()
	}
	fmt.Printf("\nNumber of files found: %v", len(allFilesList))
	fmt.Printf("\nNumber of files checked: %v\n", len(fileMap.v))
}
    

type change struct{
    path string
    oldHash string
	newHash string
}
type move struct{
	oldPath string
	newPath string
    hash string
}

type compareReport struct {
	newFiles map[string]string
	missingFiles map[string]string
	changedFiles []change
	movedFiles []move
}

func compareReports(oldReportName string, newReportName string, removeBasePath bool ){ 
	oldReport := make(map[string]string)
	newReport := make(map[string]string)  

	cr := compareReport{
        newFiles:     make(map[string]string),
        missingFiles: make(map[string]string),
        changedFiles: []change{},
        movedFiles:   []move{},
    }

	oh := reportStatFile(oldReportName) // old header
	nh := reportStatFile(newReportName) // new header

	compareReportsData(oldReportName, newReportName, oldReport, newReport, oh, nh, removeBasePath)

	fmt.Printf("\nBoth caches loaded...\n\n")

	for k, v := range oldReport {
		if v2, ok := newReport[k]; ok {
			if v2 != v {
				//fmt.Printf("ERROR - hashes don't match: %v  %v  %v\n", k, v, v2)
				cr.changedFiles = append(cr.changedFiles, change{ path: k, oldHash: v, newHash: v2 })
			}
			delete(newReport, k)           // delete, anything left is a newly found file
		} else {
			//fmt.Printf("ERROR - missing file: %v\n", k)
			cr.missingFiles[k] = v
		}
    }
	for k, v := range newReport {                // anything left is new
		//fmt.Printf("NEW FILE: %v - %v\n", k, v)
		cr.newFiles[k] = v
	}






	for k, v := range cr.missingFiles {
		for k2, v2 := range cr.newFiles {   // loop entire thing, looking for vals not keys, can't compare by path for this
            if v == v2 {                    // hashes match
                cr.movedFiles = append(cr.movedFiles, move{oldPath: k, newPath: k2, hash: v}) // add to moved
			}
		}

	
	}
	for _, v := range cr.movedFiles{                    // perfer to do it this way for now
		if _, ok := cr.missingFiles[v.oldPath]; ok {
            delete(cr.missingFiles, v.oldPath)  		// remove from old files
		}
        if _, ok := cr.newFiles[v.newPath]; ok {         // remove from new files
            delete(cr.newFiles, v.newPath)
		}
	}



	compareReportName := "compare__" + oldReportName + "__" + newReportName
	saveCompare(compareReportName, oh, nh, cr)
	fmt.Printf("\n[Completed]\n\n")
	


}

  