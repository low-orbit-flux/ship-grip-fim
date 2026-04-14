package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// syncReports pulls all reports from one agent (by alias) or all configured
// agents if alias == "".  Reports are stored at:
//   <reportDir>/remote/<alias>/<report_filename>
// Already-present files are skipped (they are immutable once written).
func syncReports(config configInfo, alias string) {
	hosts, err := parseHostsConfig(config.hostsConfig)
	if err != nil {
		fmt.Println("ERROR - reading hosts config:", err)
		return
	}
	found := false
	for _, h := range hosts {
		if alias == "" || h.alias == alias {
			found = true
			syncHostReports(config, h)
		}
	}
	if !found {
		fmt.Println("ERROR - no host found with alias:", alias)
	}
}

func syncHostReports(config configInfo, h remoteHost) {
	fmt.Printf("[%s] Fetching report list from %s:%s ...\n", h.alias, h.address, h.port)

	listOut := runRemoteCommandToString(h.address, h.port, []string{"list"})
	if strings.HasPrefix(strings.TrimSpace(listOut), "ERROR") {
		fmt.Printf("[%s] %s\n", h.alias, strings.TrimSpace(listOut))
		return
	}

	// Parse report IDs — valid filenames have no whitespace.
	var reportIDs []string
	for _, line := range strings.Split(listOut, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.ContainsAny(line, " \t") || strings.HasPrefix(line, "-") {
			continue
		}
		reportIDs = append(reportIDs, line)
	}

	if len(reportIDs) == 0 {
		fmt.Printf("[%s] No reports found on remote\n", h.alias)
		return
	}

	// Ensure local storage dir exists: <reportDir>/remote/<alias>/
	localDir := filepath.Join(config.reportDir, "remote", h.alias)
	if err := os.MkdirAll(localDir, 0755); err != nil {
		fmt.Printf("[%s] ERROR - could not create local dir %s: %v\n", h.alias, localDir, err)
		return
	}

	synced := 0
	skipped := 0
	errCount := 0
	for _, id := range reportIDs {
		localPath := filepath.Join(localDir, id)

		// Skip files that already exist — reports are immutable once written.
		if _, err := os.Stat(localPath); err == nil {
			skipped++
			continue
		}

		content := fetchRemoteReport(h.address, h.port, id)
		if strings.HasPrefix(strings.TrimSpace(content), "ERROR") {
			fmt.Printf("[%s] ERROR fetching %s: %s\n", h.alias, id, strings.TrimSpace(content))
			errCount++
			continue
		}

		if err := os.WriteFile(localPath, []byte(content), 0600); err != nil {
			fmt.Printf("[%s] ERROR saving %s: %v\n", h.alias, id, err)
			errCount++
			continue
		}
		fmt.Printf("[%s] Synced %s\n", h.alias, id)
		synced++
	}

	fmt.Printf("[%s] Done — %d synced, %d already present, %d errors\n",
		h.alias, synced, skipped, errCount)
}

// fetchRemoteReport uses the agent 'fetch' command to retrieve the raw contents
// of a single report file.
func fetchRemoteReport(address, port, reportID string) string {
	return runRemoteCommandToString(address, port, []string{"fetch", reportID})
}
