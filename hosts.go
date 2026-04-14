package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// remoteHost holds configuration for one remote agent.
// hosts.conf format (pipe-delimited, one host per line):
//   alias|address|port|path|reportName[|sshUser[|binaryPath]]
// sshUser and binaryPath are only required for the 'start' command.
type remoteHost struct {
	alias      string
	address    string
	port       string
	path       string
	reportName string
	sshUser    string
	binaryPath string
}

func parseHostsConfig(hostsConfigPath string) ([]remoteHost, error) {
	f, err := os.Open(hostsConfigPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var hosts []remoteHost
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) < 5 {
			fmt.Printf("WARN - hosts.conf line %d skipped (need at least 5 fields): %s\n", lineNum, line)
			continue
		}
		h := remoteHost{
			alias:      strings.TrimSpace(parts[0]),
			address:    strings.TrimSpace(parts[1]),
			port:       strings.TrimSpace(parts[2]),
			path:       strings.TrimSpace(parts[3]),
			reportName: strings.TrimSpace(parts[4]),
		}
		if len(parts) >= 6 {
			h.sshUser = strings.TrimSpace(parts[5])
		}
		if len(parts) >= 7 {
			h.binaryPath = strings.TrimSpace(parts[6])
		}
		hosts = append(hosts, h)
	}
	return hosts, scanner.Err()
}

// cmdHosts prints the configured host list in a table.
func cmdHosts(config configInfo) {
	hosts, err := parseHostsConfig(config.hostsConfig)
	if err != nil {
		fmt.Println("ERROR - reading hosts config:", err)
		return
	}
	if len(hosts) == 0 {
		fmt.Println("No hosts configured in", config.hostsConfig)
		return
	}
	fmt.Printf("%-18s %-20s %-6s %-25s %-20s %s\n",
		"ALIAS", "ADDRESS", "PORT", "PATH", "REPORT_NAME", "SSH_USER")
	fmt.Println(strings.Repeat("-", 100))
	for _, h := range hosts {
		fmt.Printf("%-18s %-20s %-6s %-25s %-20s %s\n",
			h.alias, h.address, h.port, h.path, h.reportName, h.sshUser)
	}
}

// hostResult carries the output of a concurrent remote operation.
type hostResult struct {
	alias  string
	output string
}

// cmdPingAll checks the status of every configured agent in parallel.
func cmdPingAll(config configInfo) {
	hosts, err := parseHostsConfig(config.hostsConfig)
	if err != nil {
		fmt.Println("ERROR - reading hosts config:", err)
		return
	}
	if len(hosts) == 0 {
		fmt.Println("No hosts configured in", config.hostsConfig)
		return
	}

	results := make([]hostResult, len(hosts))
	var wg sync.WaitGroup
	for i, h := range hosts {
		wg.Add(1)
		go func(idx int, host remoteHost) {
			defer wg.Done()
			out := runRemoteCommandToString(host.address, host.port, []string{"status"})
			results[idx] = hostResult{alias: host.alias, output: strings.TrimSpace(out)}
		}(i, h)
	}
	wg.Wait()

	fmt.Printf("%-18s %s\n", "ALIAS", "STATUS")
	fmt.Println(strings.Repeat("-", 45))
	for _, r := range results {
		fmt.Printf("%-18s %s\n", r.alias, r.output)
	}
}

// cmdRemoteAll runs cmdArgs on every configured agent in parallel and prints
// each host's output prefixed with a header.
func cmdRemoteAll(config configInfo, cmdArgs []string) {
	hosts, err := parseHostsConfig(config.hostsConfig)
	if err != nil {
		fmt.Println("ERROR - reading hosts config:", err)
		return
	}
	if len(hosts) == 0 {
		fmt.Println("No hosts configured in", config.hostsConfig)
		return
	}

	results := make([]hostResult, len(hosts))
	var wg sync.WaitGroup
	for i, h := range hosts {
		wg.Add(1)
		go func(idx int, host remoteHost) {
			defer wg.Done()
			out := runRemoteCommandToString(host.address, host.port, cmdArgs)
			results[idx] = hostResult{alias: host.alias, output: out}
		}(i, h)
	}
	wg.Wait()

	for _, r := range results {
		fmt.Printf("=== %s ===\n%s\n", r.alias, r.output)
	}
}

// cmdStartAgent starts the agent on one host (by alias) or all hosts if alias == "".
// Requires sshUser and binaryPath set in hosts.conf.
func cmdStartAgent(config configInfo, alias string) {
	hosts, err := parseHostsConfig(config.hostsConfig)
	if err != nil {
		fmt.Println("ERROR - reading hosts config:", err)
		return
	}
	found := false
	for _, h := range hosts {
		if alias == "" || h.alias == alias {
			found = true
			startAgentOnHost(h)
		}
	}
	if !found {
		fmt.Println("ERROR - no host found with alias:", alias)
	}
}

func startAgentOnHost(h remoteHost) {
	if h.sshUser == "" || h.binaryPath == "" {
		fmt.Printf("[%s] ERROR - sshUser and binaryPath must be set in hosts.conf to use 'start'\n", h.alias)
		return
	}
	target := h.sshUser + "@" + h.address
	// Run as background process on the remote host; log to /tmp
	remoteCmd := fmt.Sprintf(
		"nohup %s --agentHost=0.0.0.0 --agentPort=%s agent > /tmp/ship-grip-fim-agent.log 2>&1 &",
		h.binaryPath, h.port,
	)
	fmt.Printf("[%s] Starting agent on %s ...\n", h.alias, target)
	cmd := exec.Command("ssh", "-o", "BatchMode=yes", target, remoteCmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("[%s] ERROR - ssh failed: %v\n%s\n", h.alias, err, string(out))
		return
	}
	fmt.Printf("[%s] Agent started (port %s)\n", h.alias, h.port)
}
