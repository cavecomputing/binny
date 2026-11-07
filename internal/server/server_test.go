package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cavecomputing/binny/internal/auth"
	"github.com/cavecomputing/binny/internal/db"
	"github.com/cavecomputing/binny/internal/downloader"
	"github.com/cavecomputing/binny/internal/storage"
	"github.com/cavecomputing/binny/web"
)

const password = "correct horse battery staple"

// app is Binny on a temporary data directory, called the way a browser would.
type app struct {
	t       *testing.T
	dataDir string
	files   string // data/files/ on disk
	db      *db.DB
	handler http.Handler
	cookie  string // the session cookie it sends, "" when signed out
}

// newApp is a signed-in app on a fresh data directory.
func newApp(t *testing.T) *app {
	a := openApp(t, t.TempDir(), password)
	a.signIn()
	return a
}

// openApp is the app on dataDir with password, as it starts, not signed in.
func openApp(t *testing.T, dataDir, password string) *app {
	t.Helper()
	files := filepath.Join(dataDir, "files")
	if err := os.MkdirAll(files, 0o777); err != nil {
		t.Fatal(err)
	}
	disk, err := storage.OpenDisk(files)
	if err != nil {
		t.Fatal(err)
	}
	store, err := db.Open(filepath.Join(dataDir, "binny.db"), disk)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close(); disk.Close() })
	secret, err := store.SecretKey()
	if err != nil {
		t.Fatal(err)
	}
	login := auth.New(secret, password, web.Page("login.html"))
	handler := New(disk, store, login, downloader.New(disk, store))
	return &app{t: t, dataDir: dataDir, files: files, db: store, handler: handler}
}

func (a *app) signIn() {
	a.t.Helper()
	response := a.do("POST", "/login", url.Values{"password": {password}})
	if response.Code != http.StatusFound {
		a.t.Fatalf("signing in answered %d", response.Code)
	}
	a.cookie = sessionCookie(response)
}

// sessionCookie is the binny_session cookie a response sets, as name=value.
func sessionCookie(response *httptest.ResponseRecorder) string {
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == "binny_session" {
			return cookie.Name + "=" + cookie.Value
		}
	}
	return ""
}

// do sends a request with the app's cookie. body is nil for none, []byte for raw bytes,
// url.Values for a form, and anything else is sent as JSON. headers are name, value pairs.
func (a *app) do(method, target string, body any, headers ...string) *httptest.ResponseRecorder {
	a.t.Helper()
	var reader io.Reader
	contentType := ""
	switch body := body.(type) {
	case nil:
	case []byte:
		reader = bytes.NewReader(body)
	case url.Values:
		reader, contentType = strings.NewReader(body.Encode()), "application/x-www-form-urlencoded"
	default:
		encoded, err := json.Marshal(body)
		if err != nil {
			a.t.Fatal(err)
		}
		reader, contentType = bytes.NewReader(encoded), "application/json"
	}
	request := httptest.NewRequest(method, target, reader)
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	if a.cookie != "" {
		request.Header.Set("Cookie", a.cookie)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		request.Header.Set(headers[i], headers[i+1])
	}
	response := httptest.NewRecorder()
	a.handler.ServeHTTP(response, request)
	return response
}

// upload PUTs content to /api/upload as path inside folder.
func (a *app) upload(path, content, folder string) *httptest.ResponseRecorder {
	a.t.Helper()
	return a.do("PUT", "/api/upload?"+url.Values{"folder": {folder}, "path": {path}}.Encode(), []byte(content))
}

// listing is a folder's items by name.
func (a *app) listing(folder string) map[string]db.Item {
	a.t.Helper()
	response := a.do("GET", "/api/list?"+url.Values{"folder": {folder}}.Encode(), nil)
	if response.Code != http.StatusOK {
		a.t.Fatalf("listing %q answered %d: %s", folder, response.Code, response.Body)
	}
	var body struct{ Items []db.Item }
	decodeInto(a.t, response, &body)
	items := map[string]db.Item{}
	for _, item := range body.Items {
		items[item.Name] = item
	}
	return items
}

// itemCount is how many items a listed folder has, -1 when the listing gave no count.
func itemCount(item db.Item) int {
	if item.Items == nil {
		return -1
	}
	return *item.Items
}

// write puts content at rel under data/files/ straight on disk, making its folders.
func (a *app) write(rel, content string) {
	a.t.Helper()
	path := filepath.Join(a.files, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
		a.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o666); err != nil {
		a.t.Fatal(err)
	}
}

// read is the content of rel under data/files/, "" when there's nothing there.
func (a *app) read(rel string) string {
	content, _ := os.ReadFile(filepath.Join(a.files, rel))
	return string(content)
}

func decodeInto(t *testing.T, response *httptest.ResponseRecorder, body any) {
	t.Helper()
	if err := json.Unmarshal(response.Body.Bytes(), body); err != nil {
		t.Fatalf("not JSON (%v): %s", err, response.Body)
	}
}

// jsonOf is a response's JSON, re-encoded compactly with sorted keys so tests can compare it
// with a literal.
func jsonOf(t *testing.T, response *httptest.ResponseRecorder) string {
	t.Helper()
	var value any
	decodeInto(t, response, &value)
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

// names is what's directly inside a folder on disk, sorted.
func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	found := []string{}
	for _, entry := range entries {
		found = append(found, entry.Name())
	}
	return found
}

func TestUploadAndListSmoke(t *testing.T) {
	a := newApp(t)
	if got := jsonOf(t, a.upload("notes.txt", "hello", "")); got != `{"path":"notes.txt"}` {
		t.Fatalf("upload answered %s", got)
	}
	item := a.listing("")["notes.txt"]
	if item.Size != 5 || item.Kind != "doc" || item.IsDir {
		t.Fatalf("listed %+v", item)
	}
}
