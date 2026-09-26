package main

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestUserCommand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.db")

	if created, err := ensureDefaultUser(path); err != nil || !created {
		t.Fatalf("ensureDefaultUser = %v, %v", created, err)
	}
	if created, _ := ensureDefaultUser(path); created {
		t.Error("second ensureDefaultUser must not recreate the file")
	}
	if !usingDefaultPassword(path) {
		t.Error("fresh users file should report the default password")
	}
	if role, ok := authenticate(path, defaultAdminUser, defaultAdminPassword); !ok || role != roleAdmin {
		t.Errorf("default credentials: ok=%v role=%q", ok, role)
	}
	if _, ok := authenticate(path, defaultAdminUser, "wrong-password"); ok {
		t.Error("bad password accepted")
	}
	if _, ok := authenticate(path, "nobody", defaultAdminPassword); ok {
		t.Error("unknown user accepted")
	}

	cases := []struct{ args, want string }{
		{"add bob short", "ERROR - password must be"},
		{"add bad/name longenough", "ERROR - invalid user name"},
		{"add bob longenough banana", "ERROR - invalid role"},
		{"add bob longenough", "User bob added with role ro"},
		{"add bob longenough", "ERROR - user \"bob\" already exists"},
		{"add carol longenough rw", "User carol added with role rw"},
		{"list", "admin                admin\nbob                  ro\ncarol                rw\n"},
		{"passwd admin new-admin-pass", "Password updated for admin"},
		{"passwd ghost new-admin-pass", "ERROR - user \"ghost\" not found"},
		{"role admin ro", "ERROR - refusing to demote the last admin"},
		{"role bob admin", "Role of bob set to admin"},
		{"role admin ro", "Role of admin set to ro"},
		{"remove bob", "ERROR - refusing to remove the last admin"},
		{"role admin admin", "Role of admin set to admin"},
		{"remove bob", "User bob removed"},
		{"remove carol", "User carol removed"},
		{"bogus", "ERROR - unknown user sub-command"},
	}
	for _, c := range cases {
		got := userCommand(path, strings.Fields(c.args))
		if !strings.HasPrefix(got, c.want) {
			t.Errorf("user %s: got %q, want prefix %q", c.args, got, c.want)
		}
	}
	if usingDefaultPassword(path) {
		t.Error("default password still reported after passwd")
	}
	if _, ok := authenticate(path, defaultAdminUser, "new-admin-pass"); !ok {
		t.Error("new admin password rejected")
	}

	if st, err := os.Stat(path); err != nil || st.Mode().Perm() != 0600 {
		t.Errorf("users file perms = %v, %v", st.Mode(), err)
	}

	// Authorization wrapper: non-admins may only change their own password.
	userCommand(path, []string{"add", "dave", "daves-password", "ro"})
	if out := userCommandAs(path, []string{"list"}, "dave", roleRO); !strings.HasPrefix(out, "ERROR - permission denied") {
		t.Errorf("ro user listed users: %q", out)
	}
	if out := userCommandAs(path, []string{"passwd", "admin", "hijacked-pass"}, "dave", roleRO); !strings.HasPrefix(out, "ERROR - permission denied") {
		t.Errorf("ro user changed another password: %q", out)
	}
	if out := userCommandAs(path, []string{"passwd", "dave", "daves-new-pass"}, "dave", roleRO); !strings.HasPrefix(out, "Password updated") {
		t.Errorf("self passwd refused: %q", out)
	}

	// Legacy two-field lines are read as admin.
	legacy := filepath.Join(t.TempDir(), "users.db")
	os.WriteFile(legacy, []byte("old:$2a$10$invalidhashbutpresent\n"), 0600)
	db, _ := loadUsers(legacy)
	if db.users["old"].role != roleAdmin {
		t.Errorf("legacy line role = %q", db.users["old"].role)
	}
}

func TestCommandRole(t *testing.T) {
	cases := map[string]string{
		"status": roleRO, "list": roleRO, "data x": roleRO, "compare a b": roleRO, "quickcompare": roleRO,
		"metrics": roleRO, "jobs": roleRO, "schedule list": roleRO, "schedule history": roleRO, "whoami": roleRO,
		"scan": roleRW, "schedule add x|@daily|scan": roleRW, "schedule remove x": roleRW,
		"user list": roleAdmin, "user add x y": roleAdmin, "user remove x": roleAdmin, "user role x rw": roleAdmin,
		"user passwd me x": roleRO,
	}
	for cmd, want := range cases {
		if got := commandRole(strings.Fields(cmd)); got != want {
			t.Errorf("commandRole(%q) = %s, want %s", cmd, got, want)
		}
	}
	if roleAllows(roleRO, roleRW) || roleAllows(roleRW, roleAdmin) || !roleAllows(roleAdmin, roleRO) || roleAllows("", roleRO) {
		t.Error("roleAllows ordering wrong")
	}
}

func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
}

// TestAgentTLSAndAuth starts a real agent in-process and exercises the TLS
// pinning and login handshake through the normal client path.
func TestAgentTLSAndAuth(t *testing.T) {
	dir := t.TempDir()
	cfg := configInfo{
		dataSource:     "file",
		reportDir:      filepath.Join(dir, "reports"),
		agentHost:      "127.0.0.1",
		agentPort:      freePort(t),
		agentCert:      filepath.Join(dir, "agent.crt"),
		agentKey:       filepath.Join(dir, "agent.key"),
		knownAgents:    filepath.Join(dir, "known_agents"),
		trustNewAgents: true,
		usersDB:        filepath.Join(dir, "users.db"),
		scheduleConfig: filepath.Join(dir, "schedule.conf"),
		agentUser:      defaultAdminUser,
		agentPassword:  defaultAdminPassword,
	}

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() { startAgentServerWithStop(cfg, stop); close(done) }()
	t.Cleanup(func() { close(stop); <-done })

	// Wait for the listener.
	deadline := time.Now().Add(10 * time.Second)
	for {
		c, err := net.DialTimeout("tcp", net.JoinHostPort(cfg.agentHost, cfg.agentPort), 200*time.Millisecond)
		if err == nil {
			c.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("agent did not start listening")
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Plain (non-TLS) client must be rejected at the TLS layer.
	if c, err := net.Dial("tcp", net.JoinHostPort(cfg.agentHost, cfg.agentPort)); err == nil {
		c.Write([]byte("status\n"))
		c.SetReadDeadline(time.Now().Add(2 * time.Second))
		buf := make([]byte, 64)
		n, _ := c.Read(buf)
		if strings.Contains(string(buf[:n]), "idle") {
			t.Error("plaintext client got a command response")
		}
		c.Close()
	}

	// First connection pins the fingerprint (TOFU) and status works.
	if out := runRemoteCommandToString(cfg, cfg.agentHost, cfg.agentPort, []string{"status"}); strings.TrimSpace(out) != "idle" {
		t.Fatalf("status = %q", out)
	}
	pinned, err := os.ReadFile(cfg.knownAgents)
	if err != nil || !strings.Contains(string(pinned), "SHA256:") {
		t.Fatalf("fingerprint not pinned: %q, %v", pinned, err)
	}

	// Wrong password.
	bad := cfg
	bad.agentPassword = "not-the-password"
	if out := runRemoteCommandToString(bad, cfg.agentHost, cfg.agentPort, []string{"status"}); !strings.Contains(out, "authentication failed") {
		t.Errorf("wrong password: %q", out)
	}

	// Remote user management, then log in as the new (read-only) user.
	if out := runRemoteCommandToString(cfg, cfg.agentHost, cfg.agentPort, []string{"user", "add", "bob", "bobs-password"}); !strings.HasPrefix(out, "User bob added with role ro") {
		t.Errorf("remote user add: %q", out)
	}
	asBob := cfg
	asBob.agentUser, asBob.agentPassword = "bob", "bobs-password"
	if out := runRemoteCommandToString(asBob, cfg.agentHost, cfg.agentPort, []string{"whoami"}); strings.TrimSpace(out) != "bob ro" {
		t.Errorf("whoami as bob: %q", out)
	}
	if out := runRemoteCommandToString(asBob, cfg.agentHost, cfg.agentPort, []string{"user", "list"}); !strings.Contains(out, "permission denied") {
		t.Errorf("ro user could list users: %q", out)
	}
	if out := runRemoteCommandToString(asBob, cfg.agentHost, cfg.agentPort, []string{"scan"}); !strings.Contains(out, "requires the rw role") {
		t.Errorf("ro user could scan: %q", out)
	}
	if out := runRemoteCommandToString(asBob, cfg.agentHost, cfg.agentPort, []string{"schedule", "list"}); strings.Contains(out, "permission denied") {
		t.Errorf("ro user could not read the schedule: %q", out)
	}
	if out := runRemoteCommandToString(asBob, cfg.agentHost, cfg.agentPort, []string{"user", "passwd", "bob", "bobs-new-password"}); !strings.HasPrefix(out, "Password updated") {
		t.Errorf("self passwd as bob: %q", out)
	}
	if out := runRemoteCommandToString(cfg, cfg.agentHost, cfg.agentPort, []string{"user", "role", "bob", "rw"}); !strings.HasPrefix(out, "Role of bob set to rw") {
		t.Errorf("role change: %q", out)
	}
	asBob.agentPassword = "bobs-new-password"
	if out := runRemoteCommandToString(asBob, cfg.agentHost, cfg.agentPort, []string{"scan"}); !strings.Contains(out, "Scan") || strings.Contains(out, "permission") {
		t.Errorf("rw user could not scan: %q", out)
	}
	if out := runRemoteCommandToString(cfg, cfg.agentHost, cfg.agentPort, []string{"metrics"}); !strings.Contains(out, "default_password=1") {
		t.Errorf("metrics should flag the default password: %q", out)
	}

	// A changed fingerprint must be refused.
	addr := net.JoinHostPort(cfg.agentHost, cfg.agentPort)
	os.WriteFile(cfg.knownAgents, []byte(addr+" SHA256:"+strings.Repeat("0", 64)+"\n"), 0600)
	if out := runRemoteCommandToString(cfg, cfg.agentHost, cfg.agentPort, []string{"status"}); !strings.Contains(out, "CHANGED") {
		t.Errorf("mismatched fingerprint accepted: %q", out)
	}

	// Unknown agent with trustNewAgents=false must be refused.
	os.Remove(cfg.knownAgents)
	strict := cfg
	strict.trustNewAgents = false
	if out := runRemoteCommandToString(strict, cfg.agentHost, cfg.agentPort, []string{"status"}); !strings.Contains(out, "trustNewAgents is false") {
		t.Errorf("unknown agent accepted in strict mode: %q", out)
	}
	if _, err := os.Stat(cfg.knownAgents); err == nil {
		t.Error("strict mode must not write known_agents")
	}
}
