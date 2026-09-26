package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// Remote deploy: the Go version of scripts/deploy.sh, so the GUIs (and the
// `deploy` command) can push a binary to the hosts in hosts.conf.  For each
// host, over SSH with the user's existing keys:
//
//  1. copy the binary to binaryPath (atomically, via a .new file)
//  2. copy integrity.conf and the two ignore files next to it ONLY if they are
//     not there yet (users.db, agent.crt and agent.key are never touched)
//  3. read the agent's TLS fingerprint (creating the certificate if this is a
//     fresh install) and pin it in the local known_agents file, replacing any
//     previous entry, so the first TLS connection is verified rather than
//     trusted-on-first-use
//  4. restart the agent from the binary's directory (unless restart is off)
//
// The fingerprint is read before the restart so a fresh install has its
// certificate in place before the agent starts.

// deployOptions controls one deploy run.
type deployOptions struct {
	binary  string // local binary to push; "" = defaultDeployBinary()
	restart bool   // restart the agent after copying
}

// sshRunner runs commands and copies files on a remote host.  It is a struct
// of functions so tests can replace ssh/scp with fakes.
type sshRunner struct {
	run  func(target, cmd string) (string, error)     // ssh target cmd
	copy func(local, target, remotePath string) error // scp local target:remotePath
}

// realSSH shells out to the ssh and scp binaries, like startAgentOnHost.
var realSSH = sshRunner{
	run: func(target, cmd string) (string, error) {
		out, err := exec.Command("ssh", "-o", "BatchMode=yes", target, cmd).CombinedOutput()
		return string(out), err
	},
	copy: func(local, target, remotePath string) error {
		out, err := exec.Command("scp", "-q", "-o", "BatchMode=yes", local, target+":"+remotePath).CombinedOutput()
		if err != nil {
			return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	},
}

// defaultDeployBinary returns build/ship-grip-fim when it exists in the
// working directory (what scripts/build.sh produces), otherwise the running
// executable.
func defaultDeployBinary() string {
	if fi, err := os.Stat("build/ship-grip-fim"); err == nil && !fi.IsDir() {
		return "build/ship-grip-fim"
	}
	if exe, err := os.Executable(); err == nil {
		return exe
	}
	return "build/ship-grip-fim"
}

// deployConfigFiles lists the local config files that are installed next to
// the binary when missing on the host.
func deployConfigFiles(config configInfo) []string {
	return []string{config.configFile, config.ignorePathConfig, config.ignorePathNoWalkConfig}
}

// deployHosts deploys to the host with the given alias, or to every host when
// alias is "".  Progress goes to out.  It returns an error if any host failed.
func deployHosts(config configInfo, hosts []remoteHost, alias string, opts deployOptions, out io.Writer) error {
	if opts.binary == "" {
		opts.binary = defaultDeployBinary()
	}
	if fi, err := os.Stat(opts.binary); err != nil || fi.IsDir() {
		return fmt.Errorf("binary %s not found; run scripts/build.sh first", opts.binary)
	}
	fmt.Fprintf(out, "Deploying %s\n", opts.binary)
	deployed, failed, matched := 0, 0, false
	for _, h := range hosts {
		if alias != "" && h.alias != alias {
			continue
		}
		matched = true
		if h.sshUser == "" || h.binaryPath == "" {
			fmt.Fprintf(out, "[%s] skipped: sshUser and binaryPath must be set in %s\n", h.alias, config.hostsConfig)
			continue
		}
		if err := deployToHost(config, h, opts, realSSH, out); err != nil {
			fmt.Fprintf(out, "[%s] ERROR - %v\n", h.alias, err)
			failed++
			continue
		}
		deployed++
	}
	if !matched {
		return fmt.Errorf("no host found with alias: %s", alias)
	}
	fmt.Fprintf(out, "Done - %d host(s) deployed", deployed)
	if failed > 0 {
		fmt.Fprintf(out, ", %d failed", failed)
	}
	fmt.Fprintln(out, ".")
	if deployed > 0 {
		fmt.Fprintln(out, "Reminder: new agents start with user admin / changeme; change it with: ship-grip-fim remote <host> <port> user passwd admin <new-password>")
	}
	if failed > 0 {
		return fmt.Errorf("%d host(s) failed", failed)
	}
	return nil
}

// deployToHost performs the deploy steps for one host.
func deployToHost(config configInfo, h remoteHost, opts deployOptions, ssh sshRunner, out io.Writer) error {
	if h.sshUser == "" || h.binaryPath == "" {
		return errors.New("sshUser and binaryPath must be set in hosts.conf")
	}
	target := h.sshUser + "@" + h.address
	dir := filepath.Dir(h.binaryPath)
	fmt.Fprintf(out, "[%s] deploying to %s:%s\n", h.alias, target, h.binaryPath)

	if o, err := ssh.run(target, fmt.Sprintf("mkdir -p %q", dir)); err != nil {
		return fmt.Errorf("ssh mkdir failed: %v %s", err, strings.TrimSpace(o))
	}
	if err := ssh.copy(opts.binary, target, h.binaryPath+".new"); err != nil {
		return fmt.Errorf("copying binary: %v", err)
	}
	if o, err := ssh.run(target, fmt.Sprintf("chmod 755 %q && mv -f %q %q", h.binaryPath+".new", h.binaryPath+".new", h.binaryPath)); err != nil {
		return fmt.Errorf("installing binary: %v %s", err, strings.TrimSpace(o))
	}
	fmt.Fprintf(out, "[%s]   binary installed\n", h.alias)

	for _, f := range deployConfigFiles(config) {
		if f == "" {
			continue
		}
		if fi, err := os.Stat(f); err != nil || fi.IsDir() {
			continue
		}
		remote := dir + "/" + filepath.Base(f)
		if _, err := ssh.run(target, fmt.Sprintf("test -e %q", remote)); err == nil {
			fmt.Fprintf(out, "[%s]   %s already present, left untouched\n", h.alias, filepath.Base(f))
			continue
		}
		if err := ssh.copy(f, target, remote); err != nil {
			return fmt.Errorf("copying %s: %v", filepath.Base(f), err)
		}
		fmt.Fprintf(out, "[%s]   installed %s\n", h.alias, filepath.Base(f))
	}

	// Fingerprint first: on a fresh install this creates the certificate
	// before the agent starts, and the pin is in place before the first
	// connection.
	fpOut, err := ssh.run(target, fmt.Sprintf("cd %q && %q fingerprint", dir, h.binaryPath))
	fp := parseFingerprint(fpOut)
	if err != nil || fp == "" {
		fmt.Fprintf(out, "[%s]   WARN - could not read the agent fingerprint: %v %s\n", h.alias, err, strings.TrimSpace(fpOut))
	} else if err := pinKnownAgent(config.knownAgents, h.address+":"+h.port, fp); err != nil {
		fmt.Fprintf(out, "[%s]   WARN - could not pin fingerprint in %s: %v\n", h.alias, config.knownAgents, err)
	} else {
		fmt.Fprintf(out, "[%s]   pinned %s in %s\n", h.alias, fp, config.knownAgents)
	}

	if opts.restart {
		// Stop and start are separate ssh calls: the start command line
		// contains "<binary> ... agent" and would match the pkill pattern,
		// killing the remote shell before nohup runs.
		if o, err := ssh.run(target, fmt.Sprintf("pkill -f %q >/dev/null 2>&1; sleep 1; true", agentPkillPattern(h.binaryPath))); err != nil {
			return fmt.Errorf("stopping agent: %v %s", err, strings.TrimSpace(o))
		}
		startCmd := fmt.Sprintf("cd %q && nohup %q --agentHost=0.0.0.0 --agentPort=%s agent > agent.log 2>&1 < /dev/null &", dir, h.binaryPath, h.port)
		if o, err := ssh.run(target, startCmd); err != nil {
			return fmt.Errorf("starting agent: %v %s", err, strings.TrimSpace(o))
		}
		fmt.Fprintf(out, "[%s]   agent restarted on port %s (log: %s/agent.log)\n", h.alias, h.port, dir)
	}
	return nil
}

// agentPkillPattern returns a `pkill -f` regexp matching the agent started
// from binaryPath but not the shell running pkill itself: the last character
// of the binary name is wrapped in brackets ("...ship-grip-fi[m].* agent"),
// which the pattern's own text does not match.
func agentPkillPattern(binaryPath string) string {
	if binaryPath == "" {
		return ""
	}
	last := binaryPath[len(binaryPath)-1:]
	return binaryPath[:len(binaryPath)-1] + "[" + last + "].* agent"
}

// parseFingerprint returns the last SHA256: line of a `fingerprint` command's
// output ("" if none).
func parseFingerprint(output string) string {
	fp := ""
	s := bufio.NewScanner(strings.NewReader(output))
	for s.Scan() {
		if line := strings.TrimSpace(s.Text()); strings.HasPrefix(line, "SHA256:") {
			fp = line
		}
	}
	return fp
}

// pinKnownAgent stores fp for addr in the known_agents file, replacing any
// existing entry for that address (rememberAgent only appends).
func pinKnownAgent(path, addr, fp string) error {
	knownAgentsMu.Lock()
	defer knownAgentsMu.Unlock()
	var lines []string
	if data, err := os.ReadFile(path); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if fields := strings.Fields(line); len(fields) >= 1 && fields[0] == addr {
				continue
			}
			if strings.TrimSpace(line) != "" {
				lines = append(lines, line)
			}
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	lines = append(lines, addr+" "+fp)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(strings.Join(lines, "\n")+"\n"), 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// deployJob is a background deploy whose output the web GUI polls for.
type deployJob struct {
	mu      sync.Mutex
	running bool
	output  strings.Builder
}

var webDeployJob = &deployJob{}

// Write appends progress output; deployHosts writes to it as an io.Writer.
func (j *deployJob) Write(p []byte) (int, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.output.Write(p)
}

// start begins a deploy in the background; it fails if one is already running.
func (j *deployJob) start(config configInfo, hosts []remoteHost, alias string, opts deployOptions) error {
	j.mu.Lock()
	if j.running {
		j.mu.Unlock()
		return errors.New("a deploy is already running")
	}
	j.running = true
	j.output.Reset()
	j.mu.Unlock()
	go func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Fprintf(j, "ERROR - deploy panicked: %v\n", r)
			}
			j.mu.Lock()
			j.running = false
			j.mu.Unlock()
		}()
		if err := deployHosts(config, hosts, alias, opts, j); err != nil {
			fmt.Fprintf(j, "ERROR - %v\n", err)
		}
	}()
	return nil
}

// status returns whether a deploy is running and the output so far.
func (j *deployJob) status() (bool, string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.running, j.output.String()
}

// cmdDeploy is the `deploy [alias] [--no-restart] [--binary=PATH]` command.
func cmdDeploy(config configInfo, args []string) error {
	opts := deployOptions{restart: true}
	alias := ""
	for _, a := range args {
		switch {
		case a == "--no-restart":
			opts.restart = false
		case strings.HasPrefix(a, "--binary="):
			opts.binary = strings.TrimPrefix(a, "--binary=")
		default:
			alias = a
		}
	}
	hosts, err := parseHostsConfig(config.hostsConfig)
	if err != nil {
		return fmt.Errorf("reading hosts config: %w", err)
	}
	return deployHosts(config, hosts, alias, opts, os.Stdout)
}
