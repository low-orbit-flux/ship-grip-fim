package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"
)

// ensureReportDir creates the report directory if it does not exist.
func ensureReportDir(config configInfo) error {
	if err := os.MkdirAll(config.reportDir, 0755); err != nil {
		return fmt.Errorf("can't create report dir %q: %w", config.reportDir, err)
	}
	return nil
}

// writeLines creates path (truncating any existing file) and writes every
// line produced by fn through a buffered writer.
func writeLines(path string, fn func(w *bufio.Writer) error) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	if err := fn(w); err != nil {
		f.Close()
		return err
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func saveToDBFile(config configInfo, fileMap *SafeFileMap) error {
	if err := ensureReportDir(config); err != nil {
		return err
	}

	timeString := time.Now().Format(reportTimeFormat)
	fmt.Print(timeString)

	fileMap.mux.Lock()
	defer fileMap.mux.Unlock()

	path := config.reportDir + "/" + config.reportName + "_" + timeString
	return writeLines(path, func(w *bufio.Writer) error {
		if _, err := w.WriteString(config.reportName + "," + timeString + "," + config.host + "," + config.path + "\n"); err != nil {
			return err
		}
		for k, v := range fileMap.v {
			if _, err := w.WriteString(v + "," + k + "\n"); err != nil {
				return err
			}
		}
		return nil
	})
}

func listReportsFile(config configInfo) string {
	entries, err := os.ReadDir(config.reportDir)
	if err != nil {
		return "ERROR - can't read report dir: " + err.Error() + "\n"
	}
	var sb strings.Builder
	for _, e := range entries {
		if e.IsDir() { // e.g. the remote/ sync directory
			continue
		}
		sb.WriteString(e.Name() + "\n")
	}
	return sb.String()
}

func listReportDataFile(config configInfo, id1 string) error {
	if !validReportID(id1) {
		return fmt.Errorf("invalid report ID %q", id1)
	}
	f, err := os.Open(config.reportDir + "/" + id1)
	if err != nil {
		return err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		fmt.Println(s.Text())
	}
	return s.Err()
}

// listReportDataStringFile returns the report file contents as a string.
func listReportDataStringFile(config configInfo, id1 string) string {
	if !validReportID(id1) {
		return "ERROR - invalid report ID\n"
	}
	data, err := os.ReadFile(config.reportDir + "/" + id1)
	if err != nil {
		return "ERROR - " + err.Error() + "\n"
	}
	out := string(data)
	if out != "" && !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return out
}

func reportStatFile(config configInfo, reportNamePath string) (reportHeader, error) {
	// could have just returned this info from compareReportsDataFile() but
	// nice to have a dedicated function for other purposes
	if !validReportID(reportNamePath) {
		return reportHeader{}, fmt.Errorf("invalid report ID %q", reportNamePath)
	}
	f, err := os.Open(config.reportDir + "/" + reportNamePath)
	if err != nil {
		return reportHeader{}, err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	s.Scan()
	if err := s.Err(); err != nil {
		return reportHeader{}, err
	}
	h := strings.SplitN(s.Text(), ",", 4) // watch for commas in file names
	if len(h) < 4 {
		return reportHeader{}, fmt.Errorf("%s: header not parsed", reportNamePath)
	}
	return reportHeader{name: h[0], time: h[1], host: h[2], path: h[3]}, nil
}

// loadReportFile reads "hash,path" lines from a scan report into dst.
func loadReportFile(path string, dst map[string]string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 0, 64*1024), 1024*1024) // allow long paths
	s.Scan()                                      // skip header, caller already has it
	for s.Scan() {
		cols := strings.SplitN(s.Text(), ",", 2) // hash,path (path may contain commas)
		if len(cols) == 2 {
			dst[cols[1]] = cols[0]
		} else {
			fmt.Println("ERROR - line split in to more or less than 2 cols")
		}
	}
	return s.Err()
}

// stripBasePath rewrites every key of m with the header path removed.
func stripBasePath(m map[string]string, base string) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for _, k := range keys {
		v := m[k]
		k2 := strings.ReplaceAll(k, base, "")
		delete(m, k) // do this first (edge case), in case the base path wasn't present and the key is unchanged
		m[k2] = v
	}
}

func compareReportsDataFile(config configInfo, oldReportName string, newReportName string, oldReport map[string]string, newReport map[string]string, oldHeader reportHeader, newHeader reportHeader) error {
	fmt.Printf("\nLoading first cache...\n\n")
	if err := loadReportFile(config.reportDir+"/"+oldReportName, oldReport); err != nil {
		return err
	}

	fmt.Printf("\nLoading second cache...\n\n")
	if err := loadReportFile(config.reportDir+"/"+newReportName, newReport); err != nil {
		return err
	}

	if config.removeBasePath {
		stripBasePath(oldReport, oldHeader.path)
		stripBasePath(newReport, newHeader.path)
	}
	return nil
}

func saveCompareFile(config configInfo, compareReportName string, oldHeader reportHeader, newHeader reportHeader, cr compareReport) error {
	if err := ensureReportDir(config); err != nil {
		return err
	}

	timeString := time.Now().Format(reportTimeFormat)
	path := config.reportDir + "/" + compareReportName + "__" + timeString

	return writeLines(path, func(w *bufio.Writer) error {
		line := func(s string) error {
			fmt.Println(s)
			_, err := w.WriteString(s + "\n")
			return err
		}
		if _, err := w.WriteString("Old Header: " + oldHeader.name + "," + oldHeader.time + "," + oldHeader.host + "," + oldHeader.path + "\n"); err != nil {
			return err
		}
		if _, err := w.WriteString("New Header: " + newHeader.name + "," + newHeader.time + "," + newHeader.host + "," + newHeader.path + "\n"); err != nil {
			return err
		}
		for k, v := range cr.newFiles {
			if err := line("NEW - " + k + " - " + v); err != nil {
				return err
			}
		}
		for k, v := range cr.missingFiles {
			if err := line("MISSING - " + k + " - " + v); err != nil {
				return err
			}
		}
		for _, v := range cr.changedFiles {
			if err := line("CHANGED - " + v.path + " - " + v.oldHash + " ==> " + v.newHash); err != nil {
				return err
			}
		}
		for _, v := range cr.movedFiles {
			if err := line("MOVED - " + v.oldPath + " ==> " + v.newPath + " - " + v.hash); err != nil {
				return err
			}
		}
		return nil
	})
}
