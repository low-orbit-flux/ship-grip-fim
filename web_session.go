package main

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Web GUI login sessions.
//
// The web GUI authenticates against the same users file as the agent
// (config usersDB) and applies the same roles.  Sessions are random tokens
// kept in memory (they do not survive a restart, which is fine for an admin
// UI) and delivered as an HttpOnly, SameSite=Strict cookie.

const (
	sessionCookie = "sgf_session"
	sessionTTL    = 12 * time.Hour
)

type webSession struct {
	user    string
	role    string
	expires time.Time
}

type sessionStore struct {
	mu sync.Mutex
	m  map[string]webSession
}

var sessions = sessionStore{m: map[string]webSession{}}

func (s *sessionStore) create(user, role string) string {
	b := make([]byte, 32)
	rand.Read(b) //nolint:errcheck — crypto/rand never fails on supported platforms
	token := hex.EncodeToString(b)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for t, ses := range s.m { // opportunistic cleanup of expired sessions
		if now.After(ses.expires) {
			delete(s.m, t)
		}
	}
	s.m[token] = webSession{user: user, role: role, expires: now.Add(sessionTTL)}
	return token
}

func (s *sessionStore) get(token string) (webSession, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ses, ok := s.m[token]
	if !ok || time.Now().After(ses.expires) {
		delete(s.m, token)
		return webSession{}, false
	}
	return ses, true
}

func (s *sessionStore) delete(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, token)
}

// currentSession returns the session for the request's cookie, if valid.
func currentSession(r *http.Request) (webSession, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return webSession{}, false
	}
	return sessions.get(c.Value)
}

// requireRole wraps a handler so that only a logged-in user with at least
// role need can reach it.  need == "" means any logged-in user.
// Browser page requests are redirected to /login; API requests get JSON.
func requireRole(need string, secure bool, fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ses, ok := currentSession(r)
		if !ok {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				wErr(w, "login required", http.StatusUnauthorized)
			} else {
				http.Redirect(w, r, "/login", http.StatusFound)
			}
			return
		}
		if need != "" && !roleAllows(ses.role, need) {
			wErr(w, "permission denied: requires the "+need+" role (you are "+ses.role+")", http.StatusForbidden)
			return
		}
		// Cross-site POSTs are blocked by SameSite=Strict; additionally
		// insist on JSON bodies for API writes so HTML forms cannot be used.
		if r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/") &&
			!strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			wErr(w, "JSON body required", http.StatusUnsupportedMediaType)
			return
		}
		fn(w, r)
	}
}

// webLogin handles POST /api/login {"user":..,"password":..}.
func webLogin(config configInfo, secure bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePost(w, r) {
			return
		}
		var req struct {
			User     string `json:"user"`
			Password string `json:"password"`
		}
		if err := wDecode(r, &req); err != nil || req.User == "" {
			wErr(w, "user and password required", 400)
			return
		}
		role, ok := authenticate(config.usersDB, req.User, req.Password)
		if !ok {
			wErr(w, "login failed", http.StatusUnauthorized)
			return
		}
		token := sessions.create(req.User, role)
		http.SetCookie(w, &http.Cookie{
			Name: sessionCookie, Value: token, Path: "/",
			HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode,
			MaxAge: int(sessionTTL.Seconds()),
		})
		wJSON(w, map[string]string{"user": req.User, "role": role})
	}
}

// webLogout handles POST /api/logout.
func webLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		sessions.delete(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
	wJSON(w, map[string]string{"msg": "logged out"})
}

// webMe returns the logged-in user and role (the page uses it to enable
// only the buttons the role permits; the server enforces regardless).
func webMe(w http.ResponseWriter, r *http.Request) {
	ses, _ := currentSession(r)
	wJSON(w, map[string]string{"user": ses.user, "role": ses.role})
}

// webLoginPage serves the login form.
func webLoginPage(w http.ResponseWriter, r *http.Request) {
	if _, ok := currentSession(r); ok {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(loginHTML)) //nolint:errcheck
}

const loginHTML = `<!DOCTYPE html>
<html lang="en"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>ship-grip-fim login</title>
<style>
body{font-family:'Courier New',monospace;background:#0d1117;color:#c9d1d9;display:flex;align-items:center;justify-content:center;height:100vh;margin:0}
.box{background:#161b22;border:1px solid #30363d;border-radius:8px;padding:28px 32px;width:320px}
h1{font-size:18px;margin:0 0 18px}
label{display:block;font-size:12px;margin:10px 0 4px;color:#8b949e}
input{width:100%;box-sizing:border-box;background:#0d1117;color:#c9d1d9;border:1px solid #30363d;border-radius:4px;padding:8px;font-family:inherit}
button{margin-top:18px;width:100%;background:#238636;color:#fff;border:0;border-radius:4px;padding:9px;font-family:inherit;cursor:pointer}
#err{color:#ef4444;font-size:12px;margin-top:10px;min-height:16px}
.hint{color:#8b949e;font-size:11px;margin-top:14px}
</style></head><body>
<form class="box" onsubmit="return go(event)">
  <h1>&#x1F512; ship-grip-fim</h1>
  <label>User</label><input id="u" autocomplete="username" autofocus>
  <label>Password</label><input id="p" type="password" autocomplete="current-password">
  <button type="submit">Log in</button>
  <div id="err"></div>
  <div class="hint">Users are managed with "ship-grip-fim user ..." on the server, or by an admin on the Users tab.</div>
</form>
<script>
async function go(e){
  e.preventDefault();
  document.getElementById('err').textContent='';
  const r=await fetch('/api/login',{method:'POST',headers:{'Content-Type':'application/json'},
    body:JSON.stringify({user:document.getElementById('u').value,password:document.getElementById('p').value})});
  if(r.ok){location.href='/';}else{const d=await r.json().catch(()=>({}));document.getElementById('err').textContent=d.error||'login failed';}
  return false;
}
</script></body></html>`
