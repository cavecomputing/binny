package server

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cavecomputing/binny/internal/downloader"
)

// downloadsRemote is the far end of a download: /files/notes.txt says hello, and /slow trickles
// 1000 bytes ten at a time, for a test to catch a download in the middle.
func downloadsRemote(t *testing.T) string {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/files/notes.txt":
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte("hello"))
		case "/slow":
			w.Header().Set("Content-Length", "1000")
			for range 100 {
				if _, err := w.Write([]byte(strings.Repeat("x", 10))); err != nil {
					return
				}
				w.(http.Flusher).Flush()
				select {
				case <-r.Context().Done():
					return
				case <-time.After(20 * time.Millisecond):
				}
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server.URL
}

// downloadsStart POSTs a download and returns the job, failing unless it was accepted.
func (a *app) downloadsStart(link, folder string, tags ...string) downloader.Status {
	a.t.Helper()
	response := a.do("POST", "/api/downloads", map[string]any{"url": link, "folder": folder, "tags": tags})
	if response.Code != http.StatusCreated {
		a.t.Fatalf("POST /api/downloads answered %d: %s", response.Code, response.Body)
	}
	var job downloader.Status
	decodeInto(a.t, response, &job)
	return job
}

// downloadsWait is the job once done holds for it, polling GET /api/downloads the way the page
// does.
func (a *app) downloadsWait(id string, done func(downloader.Status) bool) downloader.Status {
	a.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var body struct{ Downloads []downloader.Status }
		decodeInto(a.t, a.do("GET", "/api/downloads", nil), &body)
		i := slices.IndexFunc(body.Downloads, func(s downloader.Status) bool { return s.ID == id })
		if i < 0 {
			a.t.Fatalf("download %s isn't listed", id)
		}
		if done(body.Downloads[i]) {
			return body.Downloads[i]
		}
		if time.Now().After(deadline) {
			a.t.Fatalf("gave up waiting: %+v", body.Downloads[i])
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestADownloadRunsInTheBackground(t *testing.T) {
	a := newApp(t)
	job := a.downloadsStart(downloadsRemote(t)+"/files/notes.txt", "", "Notes")
	if !slices.Contains([]string{"queued", "downloading", "done"}, job.State) {
		t.Fatalf("started as %q, want queued, downloading or done", job.State)
	}
	done := a.downloadsWait(job.ID, func(s downloader.Status) bool { return s.State == "done" })
	if done.Path != "notes.txt" {
		t.Fatalf("saved as %q, want notes.txt", done.Path)
	}
	if got := a.listing("")["notes.txt"].Tags; !slices.Equal(got, []string{"notes"}) {
		t.Fatalf("tags %q, want [notes]", got)
	}
	if got := jsonOf(t, a.do("POST", "/api/downloads/clear", nil)); got != `{"cleared":1}` {
		t.Fatalf("clearing answered %s, want {\"cleared\":1}", got)
	}
	if got := jsonOf(t, a.do("GET", "/api/downloads", nil)); got != `{"downloads":[]}` {
		t.Fatalf("listed %s after clearing, want none", got)
	}
}

func TestARunningDownloadCancels(t *testing.T) {
	a := newApp(t)
	job := a.downloadsStart(downloadsRemote(t)+"/slow", "")
	a.downloadsWait(job.ID, func(s downloader.Status) bool { return s.Received > 0 })
	if got, want := jsonOf(t, a.do("POST", "/api/downloads/"+job.ID+"/cancel", nil)), `{"cancelled":"`+job.ID+`"}`; got != want {
		t.Fatalf("cancelling answered %s, want %s", got, want)
	}
	if got := a.downloadsWait(job.ID, func(s downloader.Status) bool { return s.State != "cancelling" }).State; got != "cancelled" {
		t.Fatalf("state %q, want cancelled", got)
	}
	if got := names(t, a.files); len(got) != 0 {
		t.Fatalf("data/files/ holds %q, want nothing", got)
	}
	for _, id := range []string{job.ID, "nope"} {
		if got := a.do("POST", "/api/downloads/"+id+"/cancel", nil).Code; got != http.StatusNotFound {
			t.Fatalf("cancelling %s answered %d, want 404", id, got)
		}
	}
}

func TestBadRequestsAreRefused(t *testing.T) {
	for _, tt := range []struct {
		name   string
		body   map[string]any
		status int
	}{
		{"ftp", map[string]any{"url": "ftp://example.com/a.iso"}, 400},
		{"file", map[string]any{"url": "file:///etc/passwd"}, 400},
		{"no scheme", map[string]any{"url": "example.com/a.iso"}, 400},
		{"no host", map[string]any{"url": "https://"}, 400},
		{"port too big", map[string]any{"url": "https://example.com:99999/a.iso"}, 400},
		{"port zero", map[string]any{"url": "https://example.com:0/a.iso"}, 400},
		{"bad host", map[string]any{"url": "https://[::1/a.iso"}, 400},
		{"user and password", map[string]any{"url": "https://me:secret@example.com/a.iso"}, 400},
		{"no url", map[string]any{}, 400},
		{"no such folder", map[string]any{"url": "https://example.com/a.iso", "folder": "nope"}, 404},
		{"bad tag", map[string]any{"url": "https://example.com/a.iso", "tags": []string{"two words"}}, 400},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a := newApp(t)
			if got := a.do("POST", "/api/downloads", tt.body); got.Code != tt.status {
				t.Fatalf("answered %d (%s), want %d", got.Code, got.Body, tt.status)
			}
			if got := jsonOf(t, a.do("GET", "/api/downloads", nil)); got != `{"downloads":[]}` {
				t.Fatalf("listed %s, want none", got)
			}
		})
	}
}

func TestDownloadsNeedALogin(t *testing.T) {
	a := openApp(t, t.TempDir(), password)
	if got := a.do("GET", "/api/downloads", nil).Code; got != http.StatusUnauthorized {
		t.Fatalf("GET answered %d, want 401", got)
	}
	if got := a.do("POST", "/api/downloads", map[string]any{"url": "https://example.com/a"}).Code; got != http.StatusUnauthorized {
		t.Fatalf("POST answered %d, want 401", got)
	}
}
