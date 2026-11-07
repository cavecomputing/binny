package server

import (
	"archive/zip"
	"bytes"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
)

// viewsStore writes a few files straight to data/files/.
func viewsStore(a *app) {
	for name, content := range map[string]string{
		"photo.png": "\x89PNG", "page.html": "<script>alert(1)</script>", "data.bin": "\x00\x01",
		"logo.svg": "<svg/>", "movie.mp4": "0123456789", "trip/a.txt": "aaa",
		"trip/sub/b.txt": "bb", "trip/.hidden": "h",
	} {
		a.write(name, content)
	}
}

// viewsZip is the zip a response carries.
func viewsZip(t *testing.T, response *httptest.ResponseRecorder) *zip.Reader {
	t.Helper()
	body := response.Body.Bytes()
	archive, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("not a zip (%v), status %d: %s", err, response.Code, body)
	}
	return archive
}

func viewsZipNames(t *testing.T, response *httptest.ResponseRecorder) []string {
	t.Helper()
	found := []string{}
	for _, file := range viewsZip(t, response).File {
		found = append(found, file.Name)
	}
	return found
}

// viewsMediaType is a response's Content-Type without its parameters.
func viewsMediaType(response *httptest.ResponseRecorder) string {
	mediaType, _, _ := mime.ParseMediaType(response.Header().Get("Content-Type"))
	return mediaType
}

func TestFilesNeedALogin(t *testing.T) {
	a := openApp(t, t.TempDir(), password)
	viewsStore(a)
	response := a.do("GET", "/files/photo.png", nil)
	if response.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", response.Code)
	}
	if location := response.Header().Get("Location"); !strings.HasPrefix(location, "/login") {
		t.Errorf("Location = %q, want it to start with /login", location)
	}
}

func TestMediaShowsInTheTab(t *testing.T) {
	a := newApp(t)
	viewsStore(a)
	response := a.do("GET", "/files/photo.png", nil)
	if got := response.Body.String(); got != "\x89PNG" {
		t.Errorf("body = %q, want %q", got, "\x89PNG")
	}
	if got := viewsMediaType(response); got != "image/png" {
		t.Errorf("type = %q, want image/png", got)
	}
	if got := response.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "inline") {
		t.Errorf("Content-Disposition = %q, want inline", got)
	}
	if got := response.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
}

// A file replaced under the same name, even within the second, must not come from the cache.
func TestAReplacedFileIsSentAgain(t *testing.T) {
	a := newApp(t)
	a.write("photo.jpg", "old")
	first := a.do("GET", "/files/photo.jpg", nil)
	if got := first.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", got)
	}
	info, err := os.Stat(filepath.Join(a.files, "photo.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	a.write("photo.jpg", "newer")
	if err := os.Chtimes(filepath.Join(a.files, "photo.jpg"), info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	again := a.do("GET", "/files/photo.jpg", nil, "If-None-Match", first.Header().Get("ETag"), "If-Modified-Since", first.Header().Get("Last-Modified"))
	if again.Code != http.StatusOK || again.Body.String() != "newer" {
		t.Errorf("revalidating answered %d %q, want 200 \"newer\"", again.Code, again.Body)
	}
	if same := a.do("GET", "/files/photo.jpg", nil, "If-None-Match", again.Header().Get("ETag")); same.Code != http.StatusNotModified {
		t.Errorf("revalidating unchanged answered %d, want 304", same.Code)
	}
}

func TestDownloadParamForcesADownload(t *testing.T) {
	a := newApp(t)
	viewsStore(a)
	got := a.do("GET", "/files/photo.png?download", nil).Header().Get("Content-Disposition")
	if want := "attachment; filename=photo.png"; got != want {
		t.Errorf("Content-Disposition = %q, want %q", got, want)
	}
}

func TestHTMLIsSentAsPlainText(t *testing.T) {
	a := newApp(t)
	viewsStore(a)
	response := a.do("GET", "/files/page.html", nil)
	if got := response.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/plain; charset=utf-8", got)
	}
	if got := response.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "inline") {
		t.Errorf("Content-Disposition = %q, want inline", got)
	}
}

func TestSVGIsSandboxed(t *testing.T) {
	a := newApp(t)
	viewsStore(a)
	if got := a.do("GET", "/files/logo.svg", nil).Header().Get("Content-Security-Policy"); got != "sandbox" {
		t.Errorf("Content-Security-Policy = %q, want sandbox", got)
	}
}

func TestOtherTypesDownload(t *testing.T) {
	a := newApp(t)
	viewsStore(a)
	if got := a.do("GET", "/files/data.bin", nil).Header().Get("Content-Disposition"); !strings.HasPrefix(got, "attachment") {
		t.Errorf("Content-Disposition = %q, want attachment", got)
	}
}

func TestRangeRequestsSeek(t *testing.T) {
	a := newApp(t)
	viewsStore(a)
	response := a.do("GET", "/files/movie.mp4", nil, "Range", "bytes=2-4")
	if response.Code != http.StatusPartialContent {
		t.Errorf("status = %d, want 206", response.Code)
	}
	if got := response.Body.String(); got != "234" {
		t.Errorf("body = %q, want 234", got)
	}
}

func TestHiddenAndEscapingPathsAreRefused(t *testing.T) {
	for _, target := range []string{"/files/.trash/x", "/files/trip/.hidden", "/files/%2e%2e/binny.db"} {
		t.Run(target, func(t *testing.T) {
			a := newApp(t)
			viewsStore(a)
			if got := a.do("GET", target, nil).Code; got != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", got)
			}
		})
	}
}

func TestMissingFileIs404(t *testing.T) {
	a := newApp(t)
	viewsStore(a)
	if got := a.do("GET", "/files/nope.txt", nil).Code; got != http.StatusNotFound {
		t.Errorf("status = %d, want 404", got)
	}
}

// Reading a named pipe blocks until something writes to it, which would hold the thread forever.
func TestAPipeIs404AndLeftOutOfAZip(t *testing.T) {
	a := newApp(t)
	viewsStore(a)
	trip := filepath.Join(a.files, "trip")
	if err := syscall.Mkfifo(filepath.Join(trip, "pipe"), 0o666); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(trip, "a.txt"), filepath.Join(trip, "link.txt")); err != nil {
		t.Fatal(err)
	}
	if got := a.do("GET", "/files/trip/pipe", nil).Code; got != http.StatusNotFound {
		t.Errorf("pipe answered %d, want 404", got)
	}
	if got := a.do("GET", "/files/trip/link.txt", nil).Code; got != http.StatusNotFound {
		t.Errorf("symlink answered %d, want 404", got)
	}
	want := []string{"trip/", "trip/a.txt", "trip/sub/", "trip/sub/b.txt"}
	if got := viewsZipNames(t, a.do("GET", "/files/trip", nil)); !slices.Equal(got, want) {
		t.Errorf("zip holds %q, want %q", got, want)
	}
	if got := a.do("POST", "/zip", url.Values{"paths": {"trip/pipe"}}).Code; got != http.StatusNotFound {
		t.Errorf("zipping the pipe answered %d, want 404", got)
	}
}

func TestAFolderDownloadsAsAZip(t *testing.T) {
	a := newApp(t)
	viewsStore(a)
	response := a.do("GET", "/files/trip", nil)
	if got := viewsMediaType(response); got != "application/zip" {
		t.Errorf("type = %q, want application/zip", got)
	}
	if got, want := response.Header().Get("Content-Disposition"), "attachment; filename=trip.zip"; got != want {
		t.Errorf("Content-Disposition = %q, want %q", got, want)
	}
	want := []string{"trip/", "trip/a.txt", "trip/sub/", "trip/sub/b.txt"}
	if got := viewsZipNames(t, response); !slices.Equal(got, want) {
		t.Errorf("zip holds %q, want %q", got, want)
	}
	file, err := viewsZip(t, response).Open("trip/sub/b.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if content, _ := io.ReadAll(file); string(content) != "bb" {
		t.Errorf("trip/sub/b.txt holds %q, want bb", content)
	}
}

func TestASelectionDownloadsAsAZipNamedForItsFolder(t *testing.T) {
	a := newApp(t)
	viewsStore(a)
	response := a.do("POST", "/zip", url.Values{"paths": {"trip/a.txt", "trip/sub"}})
	if got := response.Header().Get("Content-Disposition"); !strings.Contains(got, "filename=trip.zip") {
		t.Errorf("Content-Disposition = %q, want filename=trip.zip in it", got)
	}
	want := []string{"a.txt", "sub/", "sub/b.txt"}
	if got := viewsZipNames(t, response); !slices.Equal(got, want) {
		t.Errorf("zip holds %q, want %q", got, want)
	}
}

func TestZipNamesKeepTheirAccents(t *testing.T) {
	a := newApp(t)
	if err := os.Mkdir(filepath.Join(a.files, "café"), 0o777); err != nil {
		t.Fatal(err)
	}
	got := a.do("GET", "/files/caf%C3%A9", nil).Header().Get("Content-Disposition")
	if want := "attachment; filename*=utf-8''caf%C3%A9.zip"; got != want {
		t.Errorf("Content-Disposition = %q, want %q", got, want)
	}
}
