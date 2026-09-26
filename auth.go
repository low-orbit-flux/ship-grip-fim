package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Authentication and roles.
//
// Users live in a small text file (config usersDB, default "users.db") with
// one "name:role:bcrypt-hash" entry per line.  Roles:
//
//	ro     read reports, compare, view schedules and metrics
//	rw     ro + run scans, add/remove scheduled jobs
//	admin  rw + manage users
//
// Every user may change their own password.  The same userCommand function
// backs the local CLI ("ship-grip-fim user ..."), the agent protocol
// ("remote <host> <port> user ...") and both GUIs, so a users file can be
// managed either on the machine it lives on or from anywhere that can
// authenticate to its agent as an admin.

const (
	roleRO    = "ro"
	roleRW    = "rw"
	roleAdmin = "admin"

	defaultAdminUser     = "admin"
	defaultAdminPassword = "changeme"
	minPasswordLen       = 8
	// authFailureDelay slows down online password guessing.
	authFailureDelay = 1 * time.Second
)

var roleRank = map[string]int{roleRO: 1, roleRW: 2, roleAdmin: 3}

func validRole(r string) bool { return roleRank[r] > 0 }

// roleAllows reports whether a user with role have may do something that
// requires role need.
func roleAllows(have, need string) bool {
	return roleRank[have] >= roleRank[need] && roleRank[need] > 0
}

// commandRole returns the role required to run an agent command line
// (parts[0] is the command).  It is the single source of truth for both the
// agent and the web GUI's /api/remote proxy.
func commandRole(parts []string) string {
	if len(parts) == 0 {
		return roleRO
	}
	switch parts[0] {
	case "scan":
		return roleRW
	case "schedule":
		if len(parts) > 1 && (parts[1] == "add" || parts[1] == "remove") {
			return roleRW
		}
		return roleRO
	case "user":
		if len(parts) > 1 && parts[1] == "passwd" {
			return roleRO // own password only; checked by userCommandAs
		}
		return roleAdmin
	default:
		// status, list, data, fetch, compare, quickcompare, metrics, jobs, whoami, ...
		return roleRO
	}
}

var userNameRe = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,32}$`)

// dummyHash is compared against when a user does not exist so that the
// response time does not reveal whether a user name is valid.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("ship-grip-fim-dummy"), bcrypt.DefaultCost)

// usersMu serialises access to the users file within this process (the agent
// and a GUI-embedded agent may both touch it).
var usersMu sync.Mutex

type userRecord struct {
	role string
	hash string
}

type userDB struct {
	path  string
	users map[string]userRecord
}

// loadUsers reads the users file.  A missing file yields an empty database
// and os.ErrNotExist so callers can decide whether that is acceptable.
// Legacy two-field lines ("name:hash") are read as admin.
func loadUsers(path string) (*userDB, error) {
	db := &userDB{path: path, users: map[string]userRecord{}}
	f, err := os.Open(path)
	if err != nil {
		return db, err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, ":")
		switch {
		case len(fields) == 3 && fields[0] != "" && validRole(fields[1]) && fields[2] != "":
			db.users[fields[0]] = userRecord{role: fields[1], hash: fields[2]}
		case len(fields) == 2 && fields[0] != "" && fields[1] != "":
			db.users[fields[0]] = userRecord{role: roleAdmin, hash: fields[1]}
		default:
			fmt.Println("WARN - skipping malformed users line for", strings.SplitN(line, ":", 2)[0])
		}
	}
	return db, s.Err()
}

// save writes the database atomically (temp file + rename) with 0600 perms.
func (db *userDB) save() error {
	if dir := filepath.Dir(db.path); dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	tmp, err := os.CreateTemp(filepath.Dir(db.path), ".users-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() { tmp.Close(); os.Remove(tmpName) }
	if err := tmp.Chmod(0600); err != nil {
		cleanup()
		return err
	}
	w := bufio.NewWriter(tmp)
	fmt.Fprintln(w, "# ship-grip-fim users: name:role:bcrypt-hash  roles: ro, rw, admin  (manage with 'ship-grip-fim user ...')")
	for _, n := range db.names() {
		fmt.Fprintf(w, "%s:%s:%s\n", n, db.users[n].role, db.users[n].hash)
	}
	if err := w.Flush(); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, db.path)
}

func (db *userDB) names() []string {
	out := make([]string, 0, len(db.users))
	for n := range db.users {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// check verifies a password and returns the user's role.
func (db *userDB) check(name, password string) (string, bool) {
	u, ok := db.users[name]
	if !ok {
		bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return "", false
	}
	if bcrypt.CompareHashAndPassword([]byte(u.hash), []byte(password)) != nil {
		return "", false
	}
	return u.role, true
}

func validatePassword(p string) error {
	if len(p) < minPasswordLen {
		return fmt.Errorf("password must be at least %d characters", minPasswordLen)
	}
	return nil
}

func (db *userDB) add(name, password, role string) error {
	if !userNameRe.MatchString(name) {
		return fmt.Errorf("invalid user name %q (letters, digits, '_', '.', '-'; max 32)", name)
	}
	if _, exists := db.users[name]; exists {
		return fmt.Errorf("user %q already exists", name)
	}
	if !validRole(role) {
		return fmt.Errorf("invalid role %q (use ro, rw or admin)", role)
	}
	if err := validatePassword(password); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	db.users[name] = userRecord{role: role, hash: string(hash)}
	return nil
}

func (db *userDB) setPassword(name, password string) error {
	u, exists := db.users[name]
	if !exists {
		return fmt.Errorf("user %q not found", name)
	}
	if err := validatePassword(password); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	u.hash = string(hash)
	db.users[name] = u
	return nil
}

func (db *userDB) setRole(name, role string) error {
	u, exists := db.users[name]
	if !exists {
		return fmt.Errorf("user %q not found", name)
	}
	if !validRole(role) {
		return fmt.Errorf("invalid role %q (use ro, rw or admin)", role)
	}
	if u.role == roleAdmin && role != roleAdmin && db.adminCount() == 1 {
		return fmt.Errorf("refusing to demote the last admin")
	}
	u.role = role
	db.users[name] = u
	return nil
}

func (db *userDB) remove(name string) error {
	u, exists := db.users[name]
	if !exists {
		return fmt.Errorf("user %q not found", name)
	}
	if u.role == roleAdmin && db.adminCount() == 1 {
		return fmt.Errorf("refusing to remove the last admin")
	}
	delete(db.users, name)
	return nil
}

func (db *userDB) adminCount() int {
	n := 0
	for _, u := range db.users {
		if u.role == roleAdmin {
			n++
		}
	}
	return n
}

// ensureDefaultUser creates the users file with the documented default
// admin account when no users file exists yet.  Returns true if it was created.
func ensureDefaultUser(path string) (bool, error) {
	usersMu.Lock()
	defer usersMu.Unlock()
	if _, err := os.Stat(path); err == nil {
		return false, nil
	}
	db := &userDB{path: path, users: map[string]userRecord{}}
	if err := db.add(defaultAdminUser, defaultAdminPassword, roleAdmin); err != nil {
		return false, err
	}
	return true, db.save()
}

// usingDefaultPassword reports whether the default admin account still has
// its default password (surfaced as a startup warning and a metric).
func usingDefaultPassword(path string) bool {
	usersMu.Lock()
	defer usersMu.Unlock()
	db, err := loadUsers(path)
	if err != nil {
		return false
	}
	_, ok := db.check(defaultAdminUser, defaultAdminPassword)
	return ok
}

// authenticate checks a name/password pair against the users file and
// returns the user's role.  Failed attempts are delayed to slow down guessing.
func authenticate(path, name, password string) (string, bool) {
	usersMu.Lock()
	db, err := loadUsers(path)
	usersMu.Unlock()
	if err != nil {
		time.Sleep(authFailureDelay)
		return "", false
	}
	role, ok := db.check(name, password)
	if !ok {
		time.Sleep(authFailureDelay)
		return "", false
	}
	return role, true
}

const userUsage = "usage: user list | user add <name> <password> [ro|rw|admin] | user passwd <name> <password> | user role <name> <ro|rw|admin> | user remove <name>\n"

// userCommand runs one "user ..." sub-command against the users file at path
// with full (local/admin) rights and returns its text output.  args excludes
// the leading "user".
func userCommand(path string, args []string) string {
	if len(args) == 0 {
		return userUsage
	}
	usersMu.Lock()
	defer usersMu.Unlock()

	db, err := loadUsers(path)
	if err != nil && !os.IsNotExist(err) {
		return "ERROR - reading users file: " + err.Error() + "\n"
	}
	saveOr := func(msg string) string {
		if err := db.save(); err != nil {
			return "ERROR - saving users file: " + err.Error() + "\n"
		}
		return msg
	}

	switch args[0] {
	case "list":
		names := db.names()
		if len(names) == 0 {
			return "No users (file " + path + " is missing or empty)\n"
		}
		var sb strings.Builder
		for _, n := range names {
			sb.WriteString(fmt.Sprintf("%-20s %s\n", n, db.users[n].role))
		}
		return sb.String()

	case "add":
		if len(args) < 3 {
			return "ERROR - usage: user add <name> <password> [ro|rw|admin]\n"
		}
		role := roleRO
		if len(args) >= 4 {
			role = args[3]
		}
		if err := db.add(args[1], args[2], role); err != nil {
			return "ERROR - " + err.Error() + "\n"
		}
		return saveOr("User " + args[1] + " added with role " + role + "\n")

	case "passwd":
		if len(args) < 3 {
			return "ERROR - usage: user passwd <name> <password>\n"
		}
		if err := db.setPassword(args[1], args[2]); err != nil {
			return "ERROR - " + err.Error() + "\n"
		}
		return saveOr("Password updated for " + args[1] + "\n")

	case "role":
		if len(args) < 3 {
			return "ERROR - usage: user role <name> <ro|rw|admin>\n"
		}
		if err := db.setRole(args[1], args[2]); err != nil {
			return "ERROR - " + err.Error() + "\n"
		}
		return saveOr("Role of " + args[1] + " set to " + args[2] + "\n")

	case "remove":
		if len(args) < 2 {
			return "ERROR - usage: user remove <name>\n"
		}
		if err := db.remove(args[1]); err != nil {
			return "ERROR - " + err.Error() + "\n"
		}
		return saveOr("User " + args[1] + " removed\n")

	default:
		return "ERROR - unknown user sub-command: " + args[0] + "\n" + userUsage
	}
}

// userCommandAs is userCommand with authorization: admins may do anything,
// everyone else may only change their own password.
func userCommandAs(path string, args []string, actor, actorRole string) string {
	if roleAllows(actorRole, roleAdmin) {
		return userCommand(path, args)
	}
	if len(args) >= 2 && args[0] == "passwd" && args[1] == actor {
		return userCommand(path, args)
	}
	return "ERROR - permission denied: managing users requires the admin role (you may only change your own password: user passwd " + actor + " <new-password>)\n"
}
