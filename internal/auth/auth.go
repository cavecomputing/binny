// Package auth is the single-password login: the sign-in page, the guard in front of every other
// route, and sign-out.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	cookieName = "binny_session" // cookies ignore the port, so it mustn't collide with sibling apps
	// "Keep this device signed in" lasts as long as browsers let a cookie (Chrome caps it at 400 days).
	rememberFor = 400 * 24 * time.Hour
	// A wrong password from any device holds off every sign-in for this long, the right password
	// included, so guessing goes at one try a second however many requests run side by side.
	wrongPasswordWait = time.Second
)

type Auth struct {
	key      []byte
	password []byte
	page     *template.Template

	mu      sync.Mutex
	nextTry time.Time // before which a sign-in is refused unchecked
}

// New is the login for password, its cookies signed with a key made from secret and the password,
// so changing the password signs every device out. page is the sign-in page.
func New(secret []byte, password string, page *template.Template) *Auth {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(password))
	return &Auth{key: mac.Sum(nil), password: []byte(password), page: page}
}

// Require lets a signed-in browser through, and the sign-in page and static files to anyone. API
// calls without a session get a 401; page loads go to the sign-in page.
func (a *Auth) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		issued, remember, ok := a.session(r)
		switch {
		case ok:
			if remember && time.Since(issued) > 24*time.Hour { // a device in use stays signed in
				a.signIn(w, r, true)
			}
			next.ServeHTTP(w, r)
		case r.URL.Path == "/login" || strings.HasPrefix(r.URL.Path, "/static/"):
			next.ServeHTTP(w, r)
		case strings.HasPrefix(r.URL.Path, "/api/"):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprintln(w, `{"error": "Sign in first"}`)
		default:
			http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusFound)
		}
	})
}

func (a *Auth) LoginPage(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := a.session(r); ok {
		http.Redirect(w, r, localTarget(r.URL.Query().Get("next")), http.StatusFound)
		return
	}
	a.render(w, http.StatusOK, "")
}

func (a *Auth) Login(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := a.session(r); ok {
		http.Redirect(w, r, localTarget(r.URL.Query().Get("next")), http.StatusFound)
		return
	}
	a.mu.Lock()
	if time.Now().Before(a.nextTry) {
		a.mu.Unlock()
		a.render(w, http.StatusTooManyRequests, "Too many wrong passwords just now. Try again in a moment.")
		return
	}
	right := subtle.ConstantTimeCompare([]byte(r.PostFormValue("password")), a.password) == 1
	if !right {
		a.nextTry = time.Now().Add(wrongPasswordWait)
	}
	a.mu.Unlock()
	if !right {
		time.Sleep(wrongPasswordWait) // so whoever mistyped can try again as soon as they see this
		a.render(w, http.StatusUnauthorized, "That password is wrong.")
		return
	}
	a.signIn(w, r, r.PostFormValue("remember") == "on")
	http.Redirect(w, r, localTarget(r.URL.Query().Get("next")), http.StatusFound)
}

func (a *Auth) Logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, "/login", http.StatusFound)
}

func (a *Auth) render(w http.ResponseWriter, status int, problem string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	a.page.Execute(w, map[string]string{"Error": problem})
}

// signIn sets the session cookie: "<issued at>.<1 if remembered>.<signature>". A remembered one
// lasts rememberFor, any other until the browser closes; neither is honoured after rememberFor.
func (a *Auth) signIn(w http.ResponseWriter, r *http.Request, remember bool) {
	flag := "0"
	if remember {
		flag = "1"
	}
	value := strconv.FormatInt(time.Now().Unix(), 10) + "." + flag
	cookie := &http.Cookie{
		Name:     cookieName,
		Value:    value + "." + a.sign(value),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		// Behind Caddy, X-Forwarded-Proto says whether the browser used HTTPS. Over plain HTTP on
		// the LAN a Secure cookie would never come back.
		Secure: r.TLS != nil || forwardedProto(r) == "https",
	}
	if remember {
		cookie.MaxAge = int(rememberFor.Seconds())
	}
	http.SetCookie(w, cookie)
}

func (a *Auth) session(r *http.Request) (issued time.Time, remember, ok bool) {
	cookie, err := r.Cookie(cookieName)
	if err != nil {
		return time.Time{}, false, false
	}
	value, signature, _ := strings.CutLast(cookie.Value, ".")
	if !hmac.Equal([]byte(signature), []byte(a.sign(value))) {
		return time.Time{}, false, false
	}
	stamp, flag, _ := strings.Cut(value, ".")
	seconds, err := strconv.ParseInt(stamp, 10, 64)
	issued = time.Unix(seconds, 0)
	return issued, flag == "1", err == nil && time.Since(issued) < rememberFor
}

func (a *Auth) sign(value string) string {
	mac := hmac.New(sha256.New, a.key)
	mac.Write([]byte(value))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func forwardedProto(r *http.Request) string {
	proto := r.Header.Get("X-Forwarded-Proto")
	return strings.TrimSpace(proto[strings.LastIndexByte(proto, ',')+1:])
}

// localTarget is target if it is a path on this site, else "/", so ?next= can't send the browser
// elsewhere. Browsers drop tabs and newlines from a URL and read a backslash as a slash, so any of
// them could turn it into "//elsewhere".
func localTarget(target string) string {
	if strings.HasPrefix(target, "/") && !strings.HasPrefix(target, "//") &&
		!strings.ContainsFunc(target, func(r rune) bool { return r == '\\' || r <= ' ' }) {
		return target
	}
	return "/"
}
