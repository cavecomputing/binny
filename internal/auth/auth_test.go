package auth

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/cavecomputing/binny/web"
)

func TestWrongPasswordHoldsOffEverySignInForASecond(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		login := New([]byte("secret"), "right", web.Page("login.html"))
		signIn := func(password string) *httptest.ResponseRecorder {
			request := httptest.NewRequest("POST", "/login", strings.NewReader(url.Values{"password": {password}}.Encode()))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			response := httptest.NewRecorder()
			login.Login(response, request)
			return response
		}

		// The wrong one sleeps out its second before it answers, so it runs beside the others.
		wrong := make(chan *httptest.ResponseRecorder)
		go func() { wrong <- signIn("nope") }()
		synctest.Wait()

		response := signIn("right") // even the right one, from anywhere
		if response.Code != http.StatusTooManyRequests {
			t.Fatalf("the right password while held off answered %d, want 429", response.Code)
		}
		if !strings.Contains(response.Body.String(), "Too many wrong passwords") {
			t.Fatalf("the page doesn't say why: %s", response.Body)
		}
		request := httptest.NewRequest("GET", "/", nil)
		for _, cookie := range response.Result().Cookies() {
			request.AddCookie(cookie)
		}
		page := httptest.NewRecorder()
		login.Require(http.NotFoundHandler()).ServeHTTP(page, request)
		if page.Code != http.StatusFound {
			t.Fatalf("GET / after a held-off sign-in answered %d, want 302", page.Code)
		}

		time.Sleep(wrongPasswordWait)
		if code := (<-wrong).Code; code != http.StatusUnauthorized {
			t.Fatalf("the wrong password answered %d, want 401", code)
		}
		if code := signIn("right").Code; code != http.StatusFound {
			t.Fatalf("the right password a second later answered %d, want 302", code)
		}
	})
}

func TestLocalTarget(t *testing.T) {
	cases := []struct{ target, want string }{
		{"/api/list?path=docs", "/api/list?path=docs"},
		{"/", "/"},
		{"", "/"},
		{"docs", "/"},
		{"//evil.example/", "/"},
		{"https://evil.example/", "/"},
		{`/\evil.example`, "/"},
		{"/\t/evil.example", "/"},
		{"/\n/evil.example", "/"},
		{"/\r\nSet-Cookie: x=1", "/"},
		{"/a b", "/"},
	}
	for _, c := range cases {
		if got := localTarget(c.target); got != c.want {
			t.Errorf("localTarget(%q) = %q, want %q", c.target, got, c.want)
		}
	}
}
