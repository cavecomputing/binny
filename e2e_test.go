package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestEndToEnd is a day in the life of Binny over real HTTP, through the same wiring as main:
// signing in, folders, uploads, tags and search, renames and moves, downloads and zips, upload
// from link, the trash, and changes made outside the app.
func TestEndToEnd(t *testing.T) {
	dataDir := t.TempDir()
	e2eWrite(t, filepath.Join(dataDir, "files", "seeded", "inner.md"), "abc") // there before it starts
	handler, closeData, err := open(dataDir, "dev")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(closeData) // after the server below closes, before the data directory goes
	server := httptest.NewServer(handler)
	defer server.Close()
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="report.pdf"`)
		io.WriteString(w, "%PDF-1.7")
	}))
	defer remote.Close()
	jar, _ := cookiejar.New(nil)
	c := &e2eClient{t: t, base: server.URL, http: &http.Client{Jar: jar}}

	// Signing in
	if response := c.request("GET", "/", nil, ""); !strings.Contains(response.Request.URL.String(), "/login?next=%2F") {
		t.Fatalf("an anonymous page load ended at %s, want the sign-in page", response.Request.URL)
	}
	c.wantStatus("GET", "/api/list", nil, "", http.StatusUnauthorized)
	c.wantStatus("POST", "/login", strings.NewReader("password=nope"), "application/x-www-form-urlencoded", http.StatusUnauthorized)
	time.Sleep(time.Second) // a wrong password holds off every sign-in for a second
	page := c.request("POST", "/login?next=%2F", strings.NewReader("password=dev&remember=on"), "application/x-www-form-urlencoded")
	if body := e2eRead(t, page); !strings.Contains(body, `src="/static/js/main.js"`) {
		t.Fatalf("signing in didn't lead to the page: %.200s", body)
	}
	c.wantStatus("GET", "/static/css/style.css", nil, "", http.StatusOK)

	// The startup scan found what was already there
	c.eventually(func() bool { return c.listing("")["seeded"] != nil }, "the startup scan to index seeded/")

	// Folders and uploads
	c.json("POST", "/api/folders", map[string]string{"parent": "", "name": "photos"}, http.StatusCreated)
	c.upload("photos", "beach.jpg", "jpeg bytes")
	c.upload("photos", "beach.jpg", "second") // never overwrites
	c.upload("", "trip/day 1/notes.txt", "hello world")
	if names := slices.Sorted(maps.Keys(c.listing("photos"))); !slices.Equal(names, []string{"beach (1).jpg", "beach.jpg"}) {
		t.Fatalf("photos holds %v", names)
	}
	if trip := c.listing("")["trip"]; trip["items"] != 1.0 || trip["size"] != 11.0 {
		t.Fatalf("trip is %v, want 1 item of 11 bytes", trip)
	}

	// Tags and search
	c.json("POST", "/api/tags", map[string]any{"paths": []string{"photos/beach.jpg", "trip"}, "add": []string{"Summer"}}, http.StatusOK)
	if got := c.search("tag=summer"); !slices.Equal(got, []string{"trip", "photos/beach.jpg"}) {
		t.Fatalf("tag search found %v", got)
	}
	if got := c.search("name=BEACH+(1)"); !slices.Equal(got, []string{"photos/beach (1).jpg"}) {
		t.Fatalf("name search found %v", got)
	}

	// Renames and moves carry tags
	c.json("POST", "/api/rename", map[string]string{"path": "trip", "name": "holiday"}, http.StatusOK)
	c.json("POST", "/api/move", map[string]any{"paths": []string{"holiday"}, "to": "photos"}, http.StatusOK)
	if got := c.search("tag=summer"); !slices.Equal(got, []string{"photos/holiday", "photos/beach.jpg"}) {
		t.Fatalf("after the move the tag finds %v", got)
	}

	// Downloads: a range of a file, a folder as a zip, a selection as a zip
	ranged := c.request("GET", "/files/photos/holiday/day%201/notes.txt", nil, "", "Range", "bytes=6-10")
	if body := e2eRead(t, ranged); ranged.StatusCode != http.StatusPartialContent || body != "world" {
		t.Fatalf("a range answered %d %q", ranged.StatusCode, body)
	}
	if got := e2eZip(t, c.request("GET", "/files/photos/holiday", nil, "")); !slices.Equal(got, []string{"holiday/", "holiday/day 1/", "holiday/day 1/notes.txt"}) {
		t.Fatalf("the folder's zip holds %v", got)
	}
	form := url.Values{"paths": {"photos/beach.jpg", "photos/holiday"}}.Encode()
	if got := e2eZip(t, c.request("POST", "/zip", strings.NewReader(form), "application/x-www-form-urlencoded")); !slices.Equal(got, []string{"beach.jpg", "holiday/", "holiday/day 1/", "holiday/day 1/notes.txt"}) {
		t.Fatalf("the selection's zip holds %v", got)
	}

	// Upload from link
	job := c.json("POST", "/api/downloads", map[string]any{"url": remote.URL + "/x", "folder": "photos", "tags": []string{"work"}}, http.StatusCreated)
	c.eventually(func() bool {
		var list struct{ Downloads []map[string]any }
		c.decode(c.request("GET", "/api/downloads", nil, ""), &list)
		return len(list.Downloads) == 1 && list.Downloads[0]["id"] == job["id"] && list.Downloads[0]["state"] == "done"
	}, "the download to finish")
	if report := c.listing("photos")["report.pdf"]; report == nil || report["tags"].([]any)[0] != "work" {
		t.Fatalf("the downloaded file is %v", report)
	}

	// The trash
	trashed := c.json("POST", "/api/trash", map[string]any{"paths": []string{"photos/holiday", "photos/report.pdf"}}, http.StatusOK)
	names := trashed["names"].([]any)
	if _, ok := c.listing("photos")["holiday"]; ok {
		t.Fatal("holiday is still listed after trashing it")
	}
	c.json("POST", "/api/trash/restore", map[string]any{"names": names[:1]}, http.StatusOK)
	if got := c.search("tag=summer"); !slices.Contains(got, "photos/holiday") {
		t.Fatalf("restored holiday lost its tag: %v", got)
	}
	c.json("POST", "/api/trash/empty", nil, http.StatusOK)
	if _, err := os.Stat(filepath.Join(dataDir, "files", "photos", "report.pdf")); !os.IsNotExist(err) {
		t.Fatal("report.pdf came back from an emptied trash")
	}

	// Changes made outside the app show on the next listing
	e2eWrite(t, filepath.Join(dataDir, "files", "photos", "added.txt"), "outside")
	os.Remove(filepath.Join(dataDir, "files", "photos", "beach (1).jpg"))
	listed := c.listing("photos")
	if listed["added.txt"] == nil || listed["beach (1).jpg"] != nil {
		t.Fatalf("outside changes not picked up: %v", slices.Sorted(maps.Keys(listed)))
	}

	// Guards: another site's write is refused, a path out of data/files too, and signing out works
	c.wantStatus("POST", "/api/folders", strings.NewReader(`{"name":"x"}`), "application/json", http.StatusForbidden, "Origin", "http://evil.example")
	c.wantStatus("GET", "/files/%2e%2e/binny.db", nil, "", http.StatusBadRequest)
	c.wantStatus("POST", "/logout", nil, "", http.StatusOK) // the redirect lands on the sign-in page
	c.wantStatus("GET", "/api/list", nil, "", http.StatusUnauthorized)
}

type e2eClient struct {
	t    *testing.T
	base string
	http *http.Client
}

// request sends a request, following redirects, with headers as name, value pairs.
func (c *e2eClient) request(method, path string, body io.Reader, contentType string, headers ...string) *http.Response {
	c.t.Helper()
	request, err := http.NewRequest(method, c.base+path, body)
	if err != nil {
		c.t.Fatal(err)
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		request.Header.Set(headers[i], headers[i+1])
	}
	response, err := c.http.Do(request)
	if err != nil {
		c.t.Fatal(err)
	}
	c.t.Cleanup(func() { response.Body.Close() })
	return response
}

func (c *e2eClient) wantStatus(method, path string, body io.Reader, contentType string, want int, headers ...string) {
	c.t.Helper()
	if response := c.request(method, path, body, contentType, headers...); response.StatusCode != want {
		c.t.Fatalf("%s %s answered %d, want %d: %s", method, path, response.StatusCode, want, e2eRead(c.t, response))
	}
}

// json sends body as JSON and returns the JSON object it answers, which must come with status.
func (c *e2eClient) json(method, path string, body any, status int) map[string]any {
	c.t.Helper()
	encoded, _ := json.Marshal(body)
	response := c.request(method, path, bytes.NewReader(encoded), "application/json")
	if response.StatusCode != status {
		c.t.Fatalf("%s %s answered %d, want %d: %s", method, path, response.StatusCode, status, e2eRead(c.t, response))
	}
	var answer map[string]any
	c.decode(response, &answer)
	return answer
}

func (c *e2eClient) decode(response *http.Response, value any) {
	c.t.Helper()
	if err := json.NewDecoder(response.Body).Decode(value); err != nil {
		c.t.Fatal(err)
	}
}

// upload sends content in chunks of unknown length, as a browser streams a big file.
func (c *e2eClient) upload(folder, path, content string) {
	c.t.Helper()
	query := url.Values{"folder": {folder}, "path": {path}}.Encode()
	response := c.request("PUT", "/api/upload?"+query, io.MultiReader(strings.NewReader(content)), "")
	if response.StatusCode != http.StatusCreated {
		c.t.Fatalf("uploading %s answered %d: %s", path, response.StatusCode, e2eRead(c.t, response))
	}
}

// listing is a folder's items by name.
func (c *e2eClient) listing(folder string) map[string]map[string]any {
	c.t.Helper()
	var body struct{ Items []map[string]any }
	c.decode(c.request("GET", "/api/list?"+url.Values{"folder": {folder}}.Encode(), nil, ""), &body)
	items := map[string]map[string]any{}
	for _, item := range body.Items {
		items[item["name"].(string)] = item
	}
	return items
}

// search is the paths a search's query string finds, in order.
func (c *e2eClient) search(query string) []string {
	c.t.Helper()
	var body struct{ Items []struct{ Path string } }
	c.decode(c.request("GET", "/api/search?"+query, nil, ""), &body)
	var paths []string
	for _, item := range body.Items {
		paths = append(paths, item.Path)
	}
	return paths
}

func (c *e2eClient) eventually(done func() bool, what string) {
	c.t.Helper()
	for deadline := time.Now().Add(10 * time.Second); !done(); time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			c.t.Fatalf("gave up waiting for %s", what)
		}
	}
}

func e2eRead(t *testing.T, response *http.Response) string {
	t.Helper()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

// e2eZip is the names in a zip response, in order.
func e2eZip(t *testing.T, response *http.Response) []string {
	t.Helper()
	body := e2eRead(t, response)
	archive, err := zip.NewReader(strings.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("not a zip (%v): %.200s", err, body)
	}
	var names []string
	for _, file := range archive.File {
		names = append(names, file.Name)
	}
	return names
}

func e2eWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o666); err != nil {
		t.Fatal(err)
	}
}
