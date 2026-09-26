package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeSSH records the ssh/scp calls deployToHost makes and answers them.
type fakeSSH struct {
	calls    []string
	existing map[string]bool // remote paths that "test -e" reports present
	fpOut    string          // output of the fingerprint command
	failCopy string          // local path whose scp fails
}

func (f *fakeSSH) runner() sshRunner {
	return sshRunner{
		run: func(target, cmd string) (string, error) {
			f.calls = append(f.calls, "ssh "+target+" "+cmd)
			switch {
			case strings.HasPrefix(cmd, "test -e "):
				path := strings.Trim(strings.TrimPrefix(cmd, "test -e "), `"`)
				if f.existing[path] {
					return "", nil
				}
				return "", errors.New("exit status 1")
			case strings.Contains(cmd, " fingerprint"):
				return f.fpOut, nil
			}
			return "", nil
		},
		copy: func(local, target, remotePath string) error {
			f.calls = append(f.calls, "scp "+filepath.Base(local)+" "+target+":"+remotePath)
			if f.failCopy != "" && local == f.failCopy {
				return errors.New("scp: connection refused")
			}
			return nil
		},
	}
}

func deployTestConfig(t *testing.T) (configInfo, string) {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "integrity.conf"), "x")
	writeFile(t, filepath.Join(dir, "integrity_ignore.cfg"), "x")
	writeFile(t, filepath.Join(dir, "integrity_ignore_no_walk.cfg"), "x")
	writeFile(t, filepath.Join(dir, "bin"), "binary")
	return configInfo{
		configFile:             filepath.Join(dir, "integrity.conf"),
		ignorePathConfig:       filepath.Join(dir, "integrity_ignore.cfg"),
		ignorePathNoWalkConfig: filepath.Join(dir, "integrity_ignore_no_walk.cfg"),
		knownAgents:            filepath.Join(dir, "known_agents"),
		hostsConfig:            "hosts.conf",
	}, filepath.Join(dir, "bin")
}

func TestDeployToHostSteps(t *testing.T) {
	config, bin := deployTestConfig(t)
	writeFile(t, config.knownAgents, "10.0.0.5:9000 SHA256:old\nother:1 SHA256:keep\n")
	fake := &fakeSSH{
		existing: map[string]bool{"/opt/sgf/integrity.conf": true},
		fpOut:    "Generated new agent certificate: agent.crt / agent.key\nSHA256:abc123\n",
	}
	h := remoteHost{alias: "offsite", address: "10.0.0.5", port: "9000", sshUser: "backup", binaryPath: "/opt/sgf/ship-grip-fim"}
	var out bytes.Buffer
	if err := deployToHost(config, h, deployOptions{binary: bin, restart: true}, fake.runner(), &out); err != nil {
		t.Fatalf("deployToHost: %v\n%s", err, out.String())
	}

	want := []string{
		`ssh backup@10.0.0.5 mkdir -p "/opt/sgf"`,
		`scp bin backup@10.0.0.5:/opt/sgf/ship-grip-fim.new`,
		`ssh backup@10.0.0.5 chmod 755 "/opt/sgf/ship-grip-fim.new" && mv -f "/opt/sgf/ship-grip-fim.new" "/opt/sgf/ship-grip-fim"`,
		`ssh backup@10.0.0.5 test -e "/opt/sgf/integrity.conf"`,
		`ssh backup@10.0.0.5 test -e "/opt/sgf/integrity_ignore.cfg"`,
		`scp integrity_ignore.cfg backup@10.0.0.5:/opt/sgf/integrity_ignore.cfg`,
		`ssh backup@10.0.0.5 test -e "/opt/sgf/integrity_ignore_no_walk.cfg"`,
		`scp integrity_ignore_no_walk.cfg backup@10.0.0.5:/opt/sgf/integrity_ignore_no_walk.cfg`,
		`ssh backup@10.0.0.5 cd "/opt/sgf" && "/opt/sgf/ship-grip-fim" fingerprint`,
		`ssh backup@10.0.0.5 pkill -f "/opt/sgf/ship-grip-fi[m].* agent" >/dev/null 2>&1; sleep 1; true`,
		`ssh backup@10.0.0.5 cd "/opt/sgf" && nohup "/opt/sgf/ship-grip-fim" --agentHost=0.0.0.0 --agentPort=9000 agent > agent.log 2>&1 < /dev/null &`,
	}
	if strings.Join(fake.calls, "\n") != strings.Join(want, "\n") {
		t.Errorf("unexpected ssh/scp sequence:\n got:\n  %s\n want:\n  %s", strings.Join(fake.calls, "\n  "), strings.Join(want, "\n  "))
	}

	for _, s := range []string{"binary installed", "integrity.conf already present", "installed integrity_ignore.cfg", "pinned SHA256:abc123", "agent restarted on port 9000"} {
		if !strings.Contains(out.String(), s) {
			t.Errorf("output missing %q:\n%s", s, out.String())
		}
	}

	known, _ := os.ReadFile(config.knownAgents)
	if got, want := string(known), "other:1 SHA256:keep\n10.0.0.5:9000 SHA256:abc123\n"; got != want {
		t.Errorf("known_agents = %q, want %q", got, want)
	}
}

func TestDeployToHostNoRestartAndMissingFields(t *testing.T) {
	config, bin := deployTestConfig(t)
	fake := &fakeSSH{fpOut: "SHA256:zzz\n"}
	h := remoteHost{alias: "a", address: "h", port: "1", sshUser: "u", binaryPath: "/x/sgf"}
	var out bytes.Buffer
	if err := deployToHost(config, h, deployOptions{binary: bin, restart: false}, fake.runner(), &out); err != nil {
		t.Fatal(err)
	}
	for _, c := range fake.calls {
		if strings.Contains(c, "pkill") || strings.Contains(c, "nohup") {
			t.Errorf("restart=false but agent was restarted: %s", c)
		}
	}
	if err := deployToHost(config, remoteHost{alias: "b", address: "h"}, deployOptions{binary: bin}, fake.runner(), &out); err == nil {
		t.Error("expected an error for a host without sshUser/binaryPath")
	}
}

func TestDeployToHostCopyFailure(t *testing.T) {
	config, bin := deployTestConfig(t)
	fake := &fakeSSH{failCopy: bin}
	h := remoteHost{alias: "a", address: "h", port: "1", sshUser: "u", binaryPath: "/x/sgf"}
	var out bytes.Buffer
	err := deployToHost(config, h, deployOptions{binary: bin, restart: true}, fake.runner(), &out)
	if err == nil || !strings.Contains(err.Error(), "copying binary") {
		t.Fatalf("expected a copy error, got %v", err)
	}
	if len(fake.calls) != 2 {
		t.Errorf("deploy should stop after the failed copy, calls: %v", fake.calls)
	}
}

func TestDeployHostsAliasFilter(t *testing.T) {
	config, bin := deployTestConfig(t)
	hosts := []remoteHost{{alias: "a", address: "h"}, {alias: "b", address: "h"}}
	var out bytes.Buffer
	if err := deployHosts(config, hosts, "nope", deployOptions{binary: bin}, &out); err == nil || !strings.Contains(err.Error(), "no host found") {
		t.Errorf("expected 'no host found' error, got %v", err)
	}
	out.Reset()
	// Hosts without sshUser/binaryPath are skipped, never contacted.
	if err := deployHosts(config, hosts, "", deployOptions{binary: bin}, &out); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "[a] skipped") || !strings.Contains(out.String(), "[b] skipped") {
		t.Errorf("expected both hosts skipped:\n%s", out.String())
	}
	if err := deployHosts(config, hosts, "", deployOptions{binary: filepath.Join(t.TempDir(), "missing")}, &out); err == nil {
		t.Error("expected an error for a missing binary")
	}
}

func TestParseFingerprintAndPkillPattern(t *testing.T) {
	if got := parseFingerprint("noise\n  SHA256:aa\nSHA256:bb\n"); got != "SHA256:bb" {
		t.Errorf("parseFingerprint = %q", got)
	}
	if got := parseFingerprint("nothing here"); got != "" {
		t.Errorf("parseFingerprint = %q, want empty", got)
	}
	if got := agentPkillPattern("/opt/sgf/ship-grip-fim"); got != "/opt/sgf/ship-grip-fi[m].* agent" {
		t.Errorf("agentPkillPattern = %q", got)
	}
}

func TestPinKnownAgentCreatesAndReplaces(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_agents")
	if err := pinKnownAgent(path, "h:1", "SHA256:one"); err != nil {
		t.Fatal(err)
	}
	if err := pinKnownAgent(path, "h:1", "SHA256:two"); err != nil {
		t.Fatal(err)
	}
	fp, found, err := lookupKnownAgent(path, "h:1")
	if err != nil || !found || fp != "SHA256:two" {
		t.Errorf("lookupKnownAgent = %q %v %v, want SHA256:two", fp, found, err)
	}
	data, _ := os.ReadFile(path)
	if strings.Count(string(data), "h:1 ") != 1 {
		t.Errorf("expected exactly one entry for h:1:\n%s", data)
	}
}

func TestDeployJobRejectsConcurrentRuns(t *testing.T) {
	config, bin := deployTestConfig(t)
	j := &deployJob{}
	j.mu.Lock()
	j.running = true
	j.mu.Unlock()
	if err := j.start(config, nil, "", deployOptions{binary: bin}); err == nil {
		t.Error("expected an error while a deploy is running")
	}
	j.mu.Lock()
	j.running = false
	j.mu.Unlock()
	running, out := j.status()
	if running || out != "" {
		t.Errorf("status = %v %q", running, out)
	}
}
