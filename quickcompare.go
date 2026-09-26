package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

// findLastTwoReports returns the two most recent scan report filenames that
// share the given reportName prefix, ordered oldest-first so callers can pass
// them directly to compareReports(config, older, newer).
func findLastTwoReports(config configInfo, reportName string) (older, newer string, err error) {
	files, err := os.ReadDir(config.reportDir)
	if err != nil {
		return "", "", fmt.Errorf("reading report dir %q: %w", config.reportDir, err)
	}

	type entry struct {
		name string
		t    time.Time
	}
	var matches []entry
	for _, f := range files {
		name := f.Name()
		if strings.HasPrefix(name, "compare__") {
			continue
		}
		if extractReportName(name) != reportName {
			continue
		}
		t := parseReportTimestamp(name)
		if t.IsZero() {
			continue
		}
		matches = append(matches, entry{name: name, t: t})
	}

	if len(matches) < 2 {
		return "", "", fmt.Errorf("need at least 2 reports named %q, found %d", reportName, len(matches))
	}

	// Sort oldest-first; take the last two.
	sort.Slice(matches, func(i, j int) bool { return matches[i].t.Before(matches[j].t) })
	n := len(matches)
	return matches[n-2].name, matches[n-1].name, nil
}

// cmdQuickCompare is the CLI handler for the "quickcompare" command.
func cmdQuickCompare(config configInfo) error {
	older, newer, err := findLastTwoReports(config, config.reportName)
	if err != nil {
		return err
	}
	fmt.Printf("Quick compare — report name: %q\n  older: %s\n  newer: %s\n\n", config.reportName, older, newer)
	return compareReports(config, older, newer)
}

// quickCompareString is the string-returning variant used by the agent server
// and web GUI.
func quickCompareString(config configInfo) string {
	older, newer, err := findLastTwoReports(config, config.reportName)
	if err != nil {
		return "ERROR - " + err.Error() + "\n"
	}
	header := fmt.Sprintf("Quick compare — report name: %q\n  older: %s\n  newer: %s\n\n",
		config.reportName, older, newer)
	return header + compareReportsString(config, older, newer)
}
