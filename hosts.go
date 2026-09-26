package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// remoteHost holds configuration for one remote agent.
// hosts.conf format (pipe-delimited, one host per line):
//
//	alias|address|port|path|reportName[|sshUser[|binaryPath[|agentUser[|agentPassword]]]]
//
// sshUser and binaryPath are only required for the 'start' command.
// agentUser / agentPassword override the agentUser / agentPassword settings
// from integrity.conf for this host only.
type remoteHost struct {
	alias         string
	address       string
	port          string
	path          string
	reportName    string
	sshUser       string
	binaryPath    string
	agentUser     string
	agentPassword string
}

// hostConfig returns config with this host's credential overrides applied.
// Every remote call for a configured host should go through it.
func hostConfig(config configInfo, h remoteHost) configInfo {
	if h.agentUser != "" {
		config.agentUser = h.agentUser
	}
	if h.agentPassword != "" {
		config.agentPassword = h.agentPassword
	}
	return config
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
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		h := remoteHost{alias: parts[0], address: parts[1], port: parts[2], path: parts[3], reportName: parts[4]}
		opt := []*string{&h.sshUser, &h.binaryPath, &h.agentUser, &h.agentPassword}
		for i, dst := range opt {
			if len(parts) > 5+i {
				*dst = parts[5+i]
			}
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
	fmt.Printf("%-18s %-20s %-6s %-25s %-20s %-10s %s\n",
		"ALIAS", "ADDRESS", "PORT", "PATH", "REPORT_NAME", "SSH_USER", "AGENT_USER")
	fmt.Println(strings.Repeat("-", 110))
	for _, h := range hosts {
		agentUser := h.agentUser
		if agentUser == "" {
			agentUser = config.agentUser + " (default)"
		}
		fmt.Printf("%-18s %-20s %-6s %-25s %-20s %-10s %s\n",
			h.alias, h.address, h.port, h.path, h.reportName, h.sshUser, agentUser)
	}
}

// hostResult carries the output of a concurrent remote operation.
type hostResult struct {
	alias  string
	output string
}

// runOnAllHosts runs cmdArgs on every host in parallel and returns the
// outputs in hosts.conf order.
func runOnAllHosts(config configInfo, hosts []remoteHost, cmdArgs []string) []hostResult {
	results := make([]hostResult, len(hosts))
	var wg sync.WaitGroup
	for i, h := range hosts {
		wg.Add(1)
		go func(idx int, host remoteHost) {
			defer wg.Done()
			out := runRemoteCommandToString(hostConfig(config, host), host.address, host.port, cmdArgs)
			results[idx] = hostResult{alias: host.alias, output: out}
		}(i, h)
	}
	wg.Wait()
	return results
}

// pingAllString returns the status table for every configured host.
func pingAllString(config configInfo, hosts []remoteHost) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%-18s %s\n", "ALIAS", "STATUS"))
	sb.WriteString(strings.Repeat("-", 45) + "\n")
	for _, r := range runOnAllHosts(config, hosts, []string{"status"}) {
		sb.WriteString(fmt.Sprintf("%-18s %s\n", r.alias, strings.TrimSpace(r.output)))
	}
	return sb.String()
}

// remoteAllString returns each host's output for cmdArgs, prefixed with a header.
func remoteAllString(config configInfo, hosts []remoteHost, cmdArgs []string) string {
	var sb strings.Builder
	for _, r := range runOnAllHosts(config, hosts, cmdArgs) {
		sb.WriteString(fmt.Sprintf("=== %s ===\n%s\n", r.alias, r.output))
	}
	return sb.String()
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
	fmt.Print(pingAllString(config, hosts))
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
	fmt.Print(remoteAllString(config, hosts, cmdArgs))
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
	// Run from the binary's directory so integrity.conf, users.db and the
	// certificate files are found next to it; log beside the binary too.
	dir := filepath.Dir(h.binaryPath)
	remoteCmd := fmt.Sprintf(
		"cd %q && nohup %q --agentHost=0.0.0.0 --agentPort=%s agent > agent.log 2>&1 &",
		dir, h.binaryPath, h.port,
	)
	fmt.Printf("[%s] Starting agent on %s ...\n", h.alias, target)
	cmd := exec.Command("ssh", "-o", "BatchMode=yes", target, remoteCmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("[%s] ERROR - ssh failed: %v\n%s\n", h.alias, err, string(out))
		return
	}
	fmt.Printf("[%s] Agent started (port %s, log: %s/agent.log)\n", h.alias, h.port, dir)
}
