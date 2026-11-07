package downloader

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cavecomputing/binny/internal/db"
	"github.com/cavecomputing/binny/internal/storage"
)

// remoteRoute is how the far end answers one path.
type remoteRoute struct {
	status  int
	headers map[string]string
	body    string
}

var remoteRoutes = map[string]remoteRoute{
	"/files/notes.txt": {200, map[string]string{"Content-Type": "text/plain"}, "hello"},
	"/my%20notes.txt":  {200, nil, "spaced"},
	"/report":          {200, map[string]string{"Content-Disposition": "attachment; filename*=UTF-8''r%C3%A9sum%C3%A9.pdf"}, "%PDF"},
	"/evil":            {200, map[string]string{"Content-Disposition": `attachment; filename="../../.evil.sh"`}, "#!"},
	"/picture":         {200, map[string]string{"Content-Type": "image/png"}, "png"},
	"/short":           {200, map[string]string{"Content-Length": "100"}, "only ten b"},
	"/moved":           {302, map[string]string{"Location": "/files/notes.txt"}, ""},
	"/to-disk":         {302, map[string]string{"Location": "file:///etc/passwd"}, ""},
	"/to-ftp":          {301, map[string]string{"Location": "ftp://127.0.0.1/file"}, ""},
	"/get.php?id=5":    {200, map[string]string{"Content-Disposition": "attachment; filename=Annual Report.pdf"}, "%PDF"},
	"/data.json.gz":    {200, map[string]string{"Content-Encoding": "gzip"}, "\x1f\x8b as it was sent"},
}

// newRemote is the far end of a download: each path answers the way a test needs, and /slow
// trickles 1000 bytes ten at a time, for a test to catch a download in the middle.
func newRemote(t *testing.T) string {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			w.Header().Set("Content-Length", "1000")
			w.WriteHeader(200)
			for range 100 {
				if _, err := w.Write([]byte(strings.Repeat("x", 10))); err != nil {
					return // the download hung up
				}
				w.(http.Flusher).Flush()
				select {
				case <-r.Context().Done():
					return
				case <-time.After(20 * time.Millisecond):
				}
			}
			return
		}
		route, ok := remoteRoutes[r.URL.RequestURI()]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header()["Content-Type"] = nil // no sniffed type: only what the route says
		w.Header().Set("Content-Length", strconv.Itoa(len(route.body)))
		for name, value := range route.headers {
			w.Header().Set(name, value)
		}
		w.WriteHeader(route.status)
		w.Write([]byte(route.body))
	}))
	t.Cleanup(server.Close)
	return server.URL
}

// testManager is a Manager on a fresh data directory, with data/files/ on disk.
type testManager struct {
	*Manager
	files string
}

func newManager(t *testing.T) testManager {
	t.Helper()
	dataDir := t.TempDir()
	files := filepath.Join(dataDir, "files")
	if err := os.Mkdir(files, 0o777); err != nil {
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
	return testManager{New(disk, store), files}
}

// waitFor is the download id once done holds for it, polling the list the way the page does.
func (m testManager) waitFor(t *testing.T, id string, done func(Status) bool) Status {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		i := slices.IndexFunc(m.List(), func(s Status) bool { return s.ID == id })
		if i < 0 {
			t.Fatalf("download %s isn't listed", id)
		}
		status := m.List()[i]
		if done(status) {
			return status
		}
		if time.Now().After(deadline) {
			t.Fatalf("gave up waiting: %+v", status)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func ended(s Status) bool {
	return s.State == "done" || s.State == "failed" || s.State == "cancelled"
}

// download starts a download of link, as the route checks it, and waits for it to end.
func (m testManager) download(t *testing.T, link, folder string, tags ...string) Status {
	t.Helper()
	checked, err := ParseURL(link)
	if err != nil {
		t.Fatalf("ParseURL(%q): %v", link, err)
	}
	return m.waitFor(t, m.Start(checked, folder, tags).ID, ended)
}

// listing is a folder's items by name, as the explorer lists them.
func (m testManager) listing(t *testing.T, folder string) map[string]db.Item {
	t.Helper()
	if err := m.db.Refresh(folder); err != nil {
		t.Fatal(err)
	}
	items, err := m.db.List(folder)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]db.Item{}
	for _, item := range items {
		byName[item.Name] = item
	}
	return byName
}

func namesIn(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	found := []string{}
	for _, entry := range entries {
		found = append(found, entry.Name())
	}
	return found
}

func TestALinkDownloadsIntoTheFolderWithItsTags(t *testing.T) {
	m := newManager(t)
	remote := newRemote(t)
	if err := m.disk.Mkdir("docs", 0o777); err != nil {
		t.Fatal(err)
	}
	job := m.download(t, remote+"/files/notes.txt", "docs", "notes")
	size := int64(-1)
	if job.Size != nil {
		size = *job.Size
	}
	if job.State != "done" || job.Path != "docs/notes.txt" || job.Name != "notes.txt" || size != 5 || job.Received != 5 || job.Error != "" {
		t.Fatalf("got %+v (size %d), want done docs/notes.txt notes.txt 5 5 with no error", job, size)
	}
	if got, _ := os.ReadFile(filepath.Join(m.files, "docs", "notes.txt")); string(got) != "hello" {
		t.Fatalf("docs/notes.txt holds %q, want %q", got, "hello")
	}
	if got := m.listing(t, "docs")["notes.txt"].Tags; !slices.Equal(got, []string{"notes"}) {
		t.Fatalf("tags %q, want [notes]", got)
	}
}

func TestTheNameItIsSavedAs(t *testing.T) {
	for _, tt := range []struct{ path, name string }{
		{"/report", "résumé.pdf"},              // the server's name for it wins
		{"/evil", "evil.sh"},                   // but it can't pick a folder or hide the file
		{"/picture", "picture.png"},            // no extension: one from the type
		{"/moved", "notes.txt"},                // redirects are followed
		{"/my notes.txt", "my notes.txt"},      // a space gets escaped on the way out, as a browser would
		{"/get.php?id=5", "Annual Report.pdf"}, // an unquoted name with a space, as some scripts send
	} {
		t.Run(tt.path, func(t *testing.T) {
			m := newManager(t)
			job := m.download(t, newRemote(t)+tt.path, "")
			if job.Path != tt.name {
				t.Fatalf("saved as %q (%+v), want %q", job.Path, job, tt.name)
			}
			if got := namesIn(t, m.files); !slices.Equal(got, []string{tt.name}) {
				t.Fatalf("data/files/ holds %q, want [%s]", got, tt.name)
			}
		})
	}
}

func TestTheFileIsSavedByteForByte(t *testing.T) {
	m := newManager(t)
	job := m.download(t, newRemote(t)+"/data.json.gz", "")
	if got, _ := os.ReadFile(filepath.Join(m.files, "data.json.gz")); job.State != "done" || string(got) != remoteRoutes["/data.json.gz"].body {
		t.Fatalf("got %+v holding %q, want the gzip bytes as sent", job, got)
	}
}

func TestATakenNameIsNumbered(t *testing.T) {
	m := newManager(t)
	remote := newRemote(t)
	m.download(t, remote+"/files/notes.txt", "")
	if got := m.download(t, remote+"/files/notes.txt", "").Path; got != "notes (1).txt" {
		t.Fatalf("second download saved as %q, want %q", got, "notes (1).txt")
	}
}

func TestAFailedDownloadLeavesNothingBehind(t *testing.T) {
	for _, tt := range []struct{ path, error string }{
		{"/missing", "The server answered 404 Not Found"},
		{"/short", "The connection closed before the end"},
		{"/to-disk", "It redirected to something other than a web address"},
		{"/to-ftp", "It redirected to something other than a web address"},
	} {
		t.Run(tt.path, func(t *testing.T) {
			m := newManager(t)
			job := m.download(t, newRemote(t)+tt.path, "")
			if job.State != "failed" || job.Error != tt.error || job.Path != "" {
				t.Fatalf("got state %q error %q path %q, want failed %q with no path", job.State, job.Error, job.Path, tt.error)
			}
			if got := namesIn(t, m.files); len(got) != 0 {
				t.Fatalf("data/files/ holds %q, want nothing", got)
			}
		})
	}
}

func TestAnUnreachableServerSaysSo(t *testing.T) {
	m := newManager(t)
	job := m.download(t, "http://127.0.0.1:9/file.iso", "")
	if job.State != "failed" {
		t.Fatalf("state %q, want failed", job.State)
	}
	if !strings.HasPrefix(job.Error, "Couldn't reach it: ") {
		t.Fatalf("error %q, want one starting %q", job.Error, "Couldn't reach it: ")
	}
}

func TestADownloadTooBigForTheDiskStops(t *testing.T) {
	m := newManager(t)
	m.keepFree = 1 << 62
	job := m.download(t, newRemote(t)+"/files/notes.txt", "")
	if job.State != "failed" || job.Error != "There isn't room for it on the disk" {
		t.Fatalf("got state %q error %q, want failed %q", job.State, job.Error, "There isn't room for it on the disk")
	}
	if got := namesIn(t, m.files); len(got) != 0 {
		t.Fatalf("data/files/ holds %q, want nothing", got)
	}
}

func TestTheListShowsNewestFirstAndClearsFinished(t *testing.T) {
	m := newManager(t)
	remote := newRemote(t)
	m.download(t, remote+"/files/notes.txt", "")
	m.download(t, remote+"/missing", "")
	var states []string
	for _, job := range m.List() {
		states = append(states, job.State)
	}
	if !slices.Equal(states, []string{"failed", "done"}) {
		t.Fatalf("listed %q, want [failed done]", states)
	}
	if got := m.Clear(); got != 2 {
		t.Fatalf("cleared %d, want 2", got)
	}
	if got := m.List(); len(got) != 0 {
		t.Fatalf("listed %+v after clearing, want nothing", got)
	}
}

func TestAQueuedDownloadCancelsAtOnce(t *testing.T) {
	m := newManager(t)
	m.slots = make(chan struct{}) // no slot ever frees, so it stays queued
	link, _ := ParseURL(newRemote(t) + "/files/notes.txt")
	job := m.Start(link, "", nil)
	if job.State != "queued" {
		t.Fatalf("state %q, want queued", job.State)
	}
	if !m.Cancel(job.ID) {
		t.Fatal("Cancel of a queued download answered false")
	}
	listed := m.List()
	if len(listed) != 1 || listed[0].State != "cancelled" {
		t.Fatalf("listed %+v, want the one download cancelled", listed)
	}
	if got := namesIn(t, m.files); len(got) != 0 {
		t.Fatalf("data/files/ holds %q, want nothing", got)
	}
	if m.Cancel(job.ID) {
		t.Fatal("cancelling it again answered true")
	}
	if m.Cancel("nope") {
		t.Fatal("cancelling an unknown download answered true")
	}
}

func TestARunningDownloadCancels(t *testing.T) {
	m := newManager(t)
	link, _ := ParseURL(newRemote(t) + "/slow")
	job := m.Start(link, "", nil)
	m.waitFor(t, job.ID, func(s Status) bool { return s.Received > 0 })
	if !m.Cancel(job.ID) {
		t.Fatal("Cancel of a running download answered false")
	}
	if got := m.waitFor(t, job.ID, func(s Status) bool { return s.State != "cancelling" }).State; got != "cancelled" {
		t.Fatalf("state %q, want cancelled", got)
	}
	if got := namesIn(t, m.files); len(got) != 0 {
		t.Fatalf("data/files/ holds %q, want nothing", got)
	}
}

func TestADownloadWhoseFolderMovesFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows won't move a folder while a file in it is open, so a download's folder can't move")
	}
	m := newManager(t)
	m.checkEvery = 1 // so the folder check comes at once, not every 64 MB
	if err := m.disk.Mkdir("docs", 0o777); err != nil {
		t.Fatal(err)
	}
	link, _ := ParseURL(newRemote(t) + "/slow")
	job := m.Start(link, "docs", nil)
	m.waitFor(t, job.ID, func(s Status) bool { return s.Received > 0 })
	if err := m.disk.Rename("docs", "papers"); err != nil {
		t.Fatal(err)
	}
	job = m.waitFor(t, job.ID, func(s Status) bool { return s.State != "downloading" })
	if job.State != "failed" || job.Error != "The folder it was going into moved" {
		t.Fatalf("got state %q error %q, want failed %q", job.State, job.Error, "The folder it was going into moved")
	}
	// It stopped rather than fetch the rest into the folder's new place.
	if job.Size == nil || job.Received >= *job.Size {
		t.Fatalf("received %d of %v, want less than all of it", job.Received, job.Size)
	}
	if got := m.listing(t, "papers"); len(got) != 0 {
		t.Fatalf("papers lists %v, want nothing", got)
	}
}

func TestParseURL(t *testing.T) {
	for _, tt := range []struct{ link, want string }{
		{"https://example.com/my notes.txt", "https://example.com/my%20notes.txt"},
		{"  https://example.com/a%20b.txt?q=x y  ", "https://example.com/a%20b.txt?q=x%20y"},
		{"https://example.com/a.iso#part", "https://example.com/a.iso"},
		{"https://example.com/100%.txt", "https://example.com/100%25.txt"},
		{"https://example.com/q%41%zz", "https://example.com/q%41%25zz"},
	} {
		got, err := ParseURL(tt.link)
		if err != nil || got != tt.want {
			t.Errorf("ParseURL(%q) = %q, %v; want %q", tt.link, got, err, tt.want)
		}
	}
}

func TestSafeName(t *testing.T) {
	for _, tt := range []struct{ name, given, want string }{
		{"evil", "../../.evil.sh", "evil.sh"},
		{"backslashes", `..\..\.evil.sh`, "evil.sh"},
		{"nothing left", "../..", "download"},
		{"long", strings.Repeat("a", 300) + ".txt", strings.Repeat("a", 236) + ".txt"},
		{"long and wide", strings.Repeat("é", 200) + ".pdf", strings.Repeat("é", 118) + ".pdf"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := safeName(tt.given); got != tt.want {
				t.Fatalf("safeName(%q) = %q (%d bytes), want %q", tt.given, got, len(got), tt.want)
			}
		})
	}
}
