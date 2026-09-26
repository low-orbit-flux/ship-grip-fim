package main

import (
	"crypto/md5"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// thread safe type used to hold hash of files after scan
type SafeFileMap struct {
	v   map[string]string
	mux sync.Mutex
}

// emptyFileMD5 is the hash of zero bytes.  Empty files have no content
// identity, so they are never paired up as "moved".
const emptyFileMD5 = "d41d8cd98f00b204e9800998ecf8427e"

// sumFile returns the hex MD5 of a file's contents.  An error is returned
// (rather than the hash of an empty stream) when the file cannot be read, so
// that unreadable files are never mistaken for empty ones.
func sumFile(file string) (string, error) {
	f, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("%s: %w", file, err)
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// matchesAny reports whether s matches at least one of the patterns.
func matchesAny(patterns []*regexp.Regexp, s string) bool {
	for _, p := range patterns {
		if p != nil && p.MatchString(s) {
			return true
		}
	}
	return false
}

// walkFiles recursively collects regular files under dir.  Directories that
// match ignorePathNoWalk are skipped entirely.  Symlinks are never followed.
func walkFiles(config configInfo, dir string, allFilesList *[]string) {
	if matchesAny(config.ignorePathNoWalk, dir) {
		fmt.Println("DEBUG: excluding " + dir)
		return
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Print(err)
		// ReadDir may return partial results alongside the error; keep going.
	}

	for _, e := range entries {
		name := e.Name()
		switch {
		case e.IsDir():
			walkFiles(config, dir+"/"+name, allFilesList) // recursive call
		case e.Type().IsRegular():
			*allFilesList = append(*allFilesList, dir+"/"+name)
		}
	}
}

// parallelFileCheck walks config.path and checksums every file using a pool
// of config.paraCount workers fed from a channel.  A channel-based pool
// balances load between workers (one huge file no longer stalls a whole
// chunk) and behaves correctly for empty directories and for any worker
// count, which the previous slice-splitting approach did not.
func parallelFileCheck(config configInfo, fileMap *SafeFileMap) {
	allFilesList := make([]string, 0, 1024)
	walkFiles(config, config.path, &allFilesList)

	workers := config.paraCount
	if workers < 1 {
		workers = 1
	}

	var (
		wg         sync.WaitGroup
		jobs       = make(chan string)
		errMu      sync.Mutex
		unreadable int
	)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for file := range jobs {
				sum, err := sumFile(file)
				if err != nil {
					log.Print("WARN - skipping unreadable file: ", err)
					errMu.Lock()
					unreadable++
					errMu.Unlock()
					continue
				}
				fileMap.mux.Lock()
				fileMap.v[file] = sum
				fileMap.mux.Unlock()
			}
		}()
	}
	for _, file := range allFilesList {
		jobs <- file
	}
	close(jobs)
	wg.Wait()

	fileMap.mux.Lock()
	for _, file := range allFilesList {
		if sum, ok := fileMap.v[file]; ok {
			fmt.Printf("%v %v\n", sum, file)
		}
	}
	checked := len(fileMap.v)
	fileMap.mux.Unlock()

	fmt.Printf("\nNumber of files found: %v", len(allFilesList))
	fmt.Printf("\nNumber of files checked: %v", checked)
	fmt.Printf("\nNumber of files unreadable (skipped): %v\n", unreadable)
}

type change struct {
	path    string
	oldHash string
	newHash string
}
type move struct {
	oldPath string
	newPath string
	hash    string
}

type compareReport struct {
	newFiles     map[string]string
	missingFiles map[string]string
	changedFiles []change
	movedFiles   []move
}

// buildCompareReport loads two reports and returns the diff result plus headers.
// It is shared by compareReports (CLI) and compareReportsString (agent).
func buildCompareReport(config configInfo, oldReportName string, newReportName string) (compareReport, reportHeader, reportHeader, error) {
	oldReport := make(map[string]string)
	newReport := make(map[string]string)

	cr := compareReport{
		newFiles:     make(map[string]string),
		missingFiles: make(map[string]string),
		changedFiles: []change{},
		movedFiles:   []move{},
	}

	oh, err := reportStat(config, oldReportName)
	if err != nil {
		return cr, oh, reportHeader{}, err
	}
	nh, err := reportStat(config, newReportName)
	if err != nil {
		return cr, oh, nh, err
	}

	if err := compareReportsData(config, oldReportName, newReportName, oldReport, newReport, oh, nh); err != nil {
		return cr, oh, nh, err
	}

	fmt.Printf("\nBoth caches loaded...\n\n")

	ignored := func(path string) bool { return matchesAny(config.ignorePath, path) }

	for k, v := range oldReport {
		if v2, ok := newReport[k]; ok {
			if v2 != v && !ignored(k) {
				cr.changedFiles = append(cr.changedFiles, change{path: k, oldHash: v, newHash: v2})
			}
			delete(newReport, k)
		} else if !ignored(k) {
			cr.missingFiles[k] = v
		}
	}
	for k, v := range newReport {
		if !ignored(k) {
			cr.newFiles[k] = v
		}
	}

	// A file that is missing at one path and new at another with the same
	// hash is reported as a move rather than as missing + new.  Matching is
	// one-to-one: each new path is consumed by at most one missing path, so
	// N identical files (e.g. empty files) that move produce N MOVED lines
	// instead of an N×N cross product.  Paths are sorted so the pairing is
	// deterministic.
	newByHash := make(map[string][]string, len(cr.newFiles))
	for k, v := range cr.newFiles {
		newByHash[v] = append(newByHash[v], k)
	}
	for _, paths := range newByHash {
		sort.Strings(paths)
	}
	missingPaths := make([]string, 0, len(cr.missingFiles))
	for k := range cr.missingFiles {
		missingPaths = append(missingPaths, k)
	}
	sort.Strings(missingPaths)
	for _, k := range missingPaths {
		v := cr.missingFiles[k]
		if v == emptyFileMD5 {
			continue // reported as MISSING + NEW instead
		}
		candidates := newByHash[v]
		if len(candidates) == 0 {
			continue
		}
		k2 := candidates[0]
		newByHash[v] = candidates[1:]
		cr.movedFiles = append(cr.movedFiles, move{oldPath: k, newPath: k2, hash: v})
		delete(cr.missingFiles, k)
		delete(cr.newFiles, k2)
	}

	// Record stats for the metrics endpoint.
	state.mu.Lock()
	state.lastCompare = &compareStats{
		OldReport: oldReportName,
		NewReport: newReportName,
		Timestamp: time.Now(),
		Changed:   len(cr.changedFiles),
		Added:     len(cr.newFiles),
		Missing:   len(cr.missingFiles),
		Moved:     len(cr.movedFiles),
	}
	state.mu.Unlock()

	return cr, oh, nh, nil
}

// compareReports runs the comparison for the local CLI (prints to stdout, saves to file).
func compareReports(config configInfo, oldReportName string, newReportName string) error {
	cr, oh, nh, err := buildCompareReport(config, oldReportName, newReportName)
	if err != nil {
		return err
	}
	compareReportName := "compare__" + oldReportName + "__" + newReportName
	if err := saveCompare(config, compareReportName, oh, nh, cr); err != nil {
		return err
	}
	fmt.Printf("\n[Completed]\n\n")
	return nil
}

// compareReportsString runs the comparison for the agent, returning the diff
// output as a string while still saving the compare report file.
func compareReportsString(config configInfo, oldReportName string, newReportName string) string {
	cr, oh, nh, err := buildCompareReport(config, oldReportName, newReportName)
	if err != nil {
		return "ERROR - " + err.Error() + "\n"
	}
	compareReportName := "compare__" + oldReportName + "__" + newReportName
	if err := saveCompare(config, compareReportName, oh, nh, cr); err != nil {
		return "ERROR - " + err.Error() + "\n"
	}

	var sb strings.Builder
	sb.WriteString("Old: " + oh.name + "," + oh.time + "," + oh.host + "," + oh.path + "\n")
	sb.WriteString("New: " + nh.name + "," + nh.time + "," + nh.host + "," + nh.path + "\n\n")
	for k, v := range cr.newFiles {
		sb.WriteString("NEW - " + k + " - " + v + "\n")
	}
	for k, v := range cr.missingFiles {
		sb.WriteString("MISSING - " + k + " - " + v + "\n")
	}
	for _, v := range cr.changedFiles {
		sb.WriteString("CHANGED - " + v.path + " - " + v.oldHash + " ==> " + v.newHash + "\n")
	}
	for _, v := range cr.movedFiles {
		sb.WriteString("MOVED - " + v.oldPath + " ==> " + v.newPath + " - " + v.hash + "\n")
	}
	return sb.String()
}
