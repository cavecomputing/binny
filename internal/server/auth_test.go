package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// authSetCookie is the raw Set-Cookie header a response gives binny_session, "" when none.
func authSetCookie(response *httptest.ResponseRecorder) string {
	for _, header := range response.Header().Values("Set-Cookie") {
		if strings.HasPrefix(header, "binny_session=") {
			return header
		}
	}
	return ""
}

func TestPagesSendYouToSignIn(t *testing.T) {
	a := openApp(t, t.TempDir(), password)
	response := a.do("GET", "/", nil)
	if response.Code != http.StatusFound {
		t.Fatalf("GET / answered %d, want 302", response.Code)
	}
	if got, want := response.Header().Get("Location"), "/login?next=%2F"; got != want {
		t.Fatalf("Location is %q, want %q", got, want)
	}
}

func TestAPIAnswers401WithoutASession(t *testing.T) {
	a := openApp(t, t.TempDir(), password)
	response := a.do("GET", "/api/list", nil)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("GET /api/list answered %d, want 401", response.Code)
	}
	if got, want := jsonOf(t, response), `{"error":"Sign in first"}`; got != want {
		t.Fatalf("body is %s, want %s", got, want)
	}
}

func TestStaticFilesNeedNoSession(t *testing.T) {
	a := openApp(t, t.TempDir(), password)
	if code := a.do("GET", "/static/css/style.css", nil).Code; code != http.StatusOK {
		t.Fatalf("GET /static/css/style.css answered %d, want 200", code)
	}
}

func TestWrongPasswordIsRefused(t *testing.T) {
	a := openApp(t, t.TempDir(), password)
	response := a.do("POST", "/login", url.Values{"password": {"nope"}})
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("a wrong password answered %d, want 401", response.Code)
	}
	if !strings.Contains(response.Body.String(), "That password is wrong.") {
		t.Fatalf("the page doesn't say the password is wrong: %s", response.Body)
	}
	if cookie := authSetCookie(response); cookie != "" {
		t.Fatalf("a wrong password set a session cookie: %s", cookie)
	}
	if code := a.do("GET", "/", nil).Code; code != http.StatusFound {
		t.Fatalf("GET / after a wrong password answered %d, want 302", code)
	}
}

func TestRememberedDeviceGetsALastingCookie(t *testing.T) {
	a := openApp(t, t.TempDir(), password)
	response := a.do("POST", "/login", url.Values{"password": {password}, "remember": {"on"}})
	if response.Code != http.StatusFound {
		t.Fatalf("signing in answered %d, want 302", response.Code)
	}
	cookie := authSetCookie(response)
	for _, attribute := range []string{"Max-Age=", "HttpOnly", "SameSite=Lax"} {
		if !strings.Contains(cookie, attribute) {
			t.Errorf("the cookie has no %s: %s", attribute, cookie)
		}
	}
	a.cookie = sessionCookie(response)
	if code := a.do("GET", "/", nil).Code; code != http.StatusOK {
		t.Fatalf("GET / signed in answered %d, want 200", code)
	}
}

func TestUnrememberedDeviceGetsABrowserSessionCookie(t *testing.T) {
	a := openApp(t, t.TempDir(), password)
	response := a.do("POST", "/login", url.Values{"password": {password}})
	if cookie := authSetCookie(response); cookie == "" || strings.Contains(cookie, "Max-Age=") || strings.Contains(cookie, "Expires=") {
		t.Fatalf("want a cookie that ends with the browser session, got %q", cookie)
	}
	a.cookie = sessionCookie(response)
	if code := a.do("GET", "/", nil).Code; code != http.StatusOK {
		t.Fatalf("GET / signed in answered %d, want 200", code)
	}
}

func TestSignInReturnsOnlyToThisSite(t *testing.T) {
	cases := []struct{ target, want string }{
		{"/api/list?path=docs", "/api/list?path=docs"},
		{"//evil.example/", "/"},
		{"https://evil.example/", "/"},
		{`/\evil.example`, "/"},
		{"/\t/evil.example", "/"},
		{"/\n/evil.example", "/"},
		{"/\r\nSet-Cookie: x=1", "/"},
	}
	for _, c := range cases {
		t.Run(c.target, func(t *testing.T) {
			a := openApp(t, t.TempDir(), password)
			response := a.do("POST", "/login?"+url.Values{"next": {c.target}}.Encode(), url.Values{"password": {password}})
			if got := response.Header().Get("Location"); got != c.want {
				t.Fatalf("next=%q went to %q, want %q", c.target, got, c.want)
			}
		})
	}
}

func TestSignOut(t *testing.T) {
	a := newApp(t)
	if code := a.do("GET", "/", nil).Code; code != http.StatusOK {
		t.Fatalf("GET / signed in answered %d, want 200", code)
	}
	response := a.do("POST", "/logout", nil)
	deleted := false
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == "binny_session" && cookie.MaxAge < 0 {
			deleted = true
		}
	}
	if !deleted {
		t.Fatalf("signing out didn't delete the cookie: %q", response.Header().Values("Set-Cookie"))
	}
	a.cookie = "" // as the browser does with a deleted cookie
	if code := a.do("GET", "/", nil).Code; code != http.StatusFound {
		t.Fatalf("GET / signed out answered %d, want 302", code)
	}
}

func TestCookieSurvivesARestart(t *testing.T) {
	a := newApp(t)
	restarted := openApp(t, a.dataDir, password)
	restarted.cookie = a.cookie
	if code := restarted.do("GET", "/", nil).Code; code != http.StatusOK {
		t.Fatalf("GET / after a restart answered %d, want 200", code)
	}
}

func TestNewPasswordSignsEveryDeviceOut(t *testing.T) {
	a := newApp(t)
	restarted := openApp(t, a.dataDir, "a new password")
	restarted.cookie = a.cookie
	if code := restarted.do("GET", "/", nil).Code; code != http.StatusFound {
		t.Fatalf("GET / under a new password answered %d, want 302", code)
	}
}

func TestCookieIsSecureWhenCaddySaysHTTPS(t *testing.T) {
	a := openApp(t, t.TempDir(), password)
	response := a.do("POST", "/login", url.Values{"password": {password}}, "X-Forwarded-Proto", "https")
	if cookie := authSetCookie(response); !strings.Contains(cookie, "Secure") {
		t.Errorf("over HTTPS the cookie isn't Secure: %q", cookie)
	}
	response = a.do("POST", "/login", url.Values{"password": {password}})
	if cookie := authSetCookie(response); cookie == "" || strings.Contains(cookie, "Secure") {
		t.Errorf("over plain HTTP want a cookie that isn't Secure, got %q", cookie)
	}
}

func TestCrossSiteWritesAreBlocked(t *testing.T) {
	a := newApp(t)
	if code := a.do("POST", "/logout", nil, "Sec-Fetch-Site", "cross-site").Code; code != http.StatusForbidden {
		t.Errorf("a cross-site sign-out answered %d, want 403", code)
	}
	if code := a.do("POST", "/logout", nil, "Origin", "http://evil.example").Code; code != http.StatusForbidden {
		t.Errorf("a sign-out from another origin answered %d, want 403", code)
	}
	if got := jsonOf(t, a.do("POST", "/api/folders", map[string]string{"parent": "", "name": "x"}, "Origin", "http://evil.example")); got != `{"error":"Cross-site request blocked"}` {
		t.Errorf("a cross-site API write answered %s, want Binny's JSON error", got)
	}
	if code := a.do("GET", "/", nil).Code; code != http.StatusOK {
		t.Fatalf("GET / after the refused sign-outs answered %d, want 200", code)
	}
}
