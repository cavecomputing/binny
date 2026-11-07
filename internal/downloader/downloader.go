// Package downloader is upload from link: the server fetches a URL into a folder, off the request.
//
// Jobs live in memory, which is why Binny is one process: the list starts empty after a restart,
// and what was downloaded stays as ordinary files. A download streams into a partial file beside
// its target and is renamed into place once complete, like an upload. The remote server never
// picks the path: its name for the file goes through the same checks as an upload's.
package downloader

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/cavecomputing/binny/internal/db"
	"github.com/cavecomputing/binny/internal/storage"
)

const (
	atOnce       = 3 // downloads running side by side; the rest wait their turn
	timeout      = 30 * time.Second
	chunk        = 1 << 20
	keepFree     = 1 << 30 // a download stops rather than leave the disk with less than this
	finishedKept = 50      // finished downloads the list remembers
)

// Status is a download as the page lists it.
type Status struct {
	ID       string  `json:"id"`
	URL      string  `json:"url"`
	Folder   string  `json:"folder"`
	Name     string  `json:"name"`
	State    string  `json:"state"` // queued, downloading, cancelling, done, failed or cancelled
	Size     *int64  `json:"size"`  // null until the server says
	Received int64   `json:"received"`
	Path     string  `json:"path"`
	Error    string  `json:"error"`
	Rate     float64 `json:"rate"` // bytes a second
}

type job struct {
	Status
	tags           []string
	started, ended time.Time
	ctx            context.Context
	cancel         context.CancelFunc
}

// Manager runs the downloads and keeps the list of them.
type Manager struct {
	disk     *storage.Disk
	db       *db.DB
	client   *http.Client
	slots    chan struct{} // a queued download waits for one
	keepFree uint64
	// checkEvery is how many bytes a download writes between checks of the disk and its folder.
	checkEvery int64

	mu   sync.Mutex // held to change a job, or the list
	jobs []*job     // oldest first
}

// stopped is a download Binny gave up on; the text says why.
type stopped string

func (s stopped) Error() string { return string(s) }

func New(disk *storage.Disk, store *db.DB) *Manager {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = (&net.Dialer{Timeout: timeout}).DialContext
	transport.TLSHandshakeTimeout = timeout
	transport.ResponseHeaderTimeout = timeout
	return &Manager{
		disk: disk,
		db:   store,
		client: &http.Client{
			Transport: transport,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
					return stopped("It redirected to something other than a web address")
				}
				if len(via) >= 10 {
					return stopped("It redirected too many times")
				}
				return nil
			},
		},
		slots:      make(chan struct{}, atOnce),
		keepFree:   keepFree,
		checkEvery: 64 * chunk,
	}
}

// Start queues a download of link into folder, to be tagged with tags.
func (m *Manager) Start(link, folder string, tags []string) Status {
	u, _ := url.Parse(link)
	ctx, cancel := context.WithCancel(context.Background())
	id := make([]byte, 16)
	rand.Read(id)
	j := &job{ID: hex.EncodeToString(id), URL: link, Folder: folder, Name: nameInURL(u), State: "queued",
		tags: tags, ctx: ctx, cancel: cancel}
	m.mu.Lock()
	m.jobs = append(m.jobs, j)
	excess := -finishedKept
	for _, old := range m.jobs {
		if !old.ended.IsZero() {
			excess++
		}
	}
	m.jobs = slices.DeleteFunc(m.jobs, func(old *job) bool {
		if excess > 0 && !old.ended.IsZero() {
			excess--
			return true
		}
		return false
	})
	status := m.status(j)
	m.mu.Unlock()
	go m.run(j)
	return status
}

// List is every download, newest first.
func (m *Manager) List() []Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	list := make([]Status, 0, len(m.jobs))
	for _, j := range slices.Backward(m.jobs) {
		list = append(list, m.status(j))
	}
	return list
}

// status is j as the page sees it. Hold mu.
func (m *Manager) status(j *job) Status {
	status := j.Status
	if !j.started.IsZero() {
		end := j.ended
		if end.IsZero() {
			end = time.Now()
		}
		if elapsed := end.Sub(j.started).Seconds(); elapsed > 0 {
			status.Rate = float64(j.Received) / elapsed
		}
	}
	if j.ctx.Err() != nil && j.ended.IsZero() {
		status.State = "cancelling"
	}
	return status
}

// Cancel stops a download that hasn't finished. It returns false when there's none such.
func (m *Manager) Cancel(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	i := slices.IndexFunc(m.jobs, func(j *job) bool { return j.ID == id })
	if i < 0 || !m.jobs[i].ended.IsZero() {
		return false
	}
	j := m.jobs[i]
	j.cancel()
	if j.State == "queued" { // run() will find it cancelled
		j.State, j.ended = "cancelled", time.Now()
	}
	return true
}

// Clear forgets the finished downloads and returns how many there were.
func (m *Manager) Clear() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	before := len(m.jobs)
	m.jobs = slices.DeleteFunc(m.jobs, func(j *job) bool { return !j.ended.IsZero() })
	return before - len(m.jobs)
}

// run waits for a slot, then downloads j and records how it ended. A restart doesn't wait for it;
// one cut short that way leaves a partial file, which a later scan of its folder deletes.
func (m *Manager) run(j *job) {
	select {
	case m.slots <- struct{}{}:
	case <-j.ctx.Done():
		return
	}
	defer func() { <-m.slots }()
	m.mu.Lock()
	if j.ctx.Err() != nil { // cancelled while it waited
		m.mu.Unlock()
		return
	}
	j.State, j.started = "downloading", time.Now()
	m.mu.Unlock()
	err := m.fetch(j)
	m.mu.Lock()
	defer m.mu.Unlock()
	switch {
	case err == nil:
		j.State = "done"
	case j.ctx.Err() != nil && !errors.Is(err, errStalled):
		j.State = "cancelled"
	default:
		j.State, j.Error = "failed", m.failure(j, err)
	}
	j.ended = time.Now()
	j.cancel()
}

var errStalled = errors.New("It stopped sending")

func (m *Manager) fetch(j *job) error {
	ctx, stall := context.WithCancelCause(j.ctx)
	defer stall(nil)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, j.URL, nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", "binny")
	response, err := m.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return stopped("The server answered " + response.Status)
	}
	name := nameFrom(response)
	m.mu.Lock()
	j.Name = name
	if size := response.ContentLength; size >= 0 {
		j.Size = &size
	}
	m.mu.Unlock()
	if !m.disk.IsFolder(j.Folder) {
		return stopped("The folder it was going into is gone")
	}
	if response.ContentLength > 0 && uint64(response.ContentLength) > m.room() {
		return stopped("There isn't room for it on the disk")
	}

	partial := storage.Child(j.Folder, storage.PartialName())
	out, err := m.disk.OpenFile(partial, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
	if err != nil {
		return err
	}
	defer m.disk.Remove(partial)
	defer out.Close()
	// Each read gets the timeout: one that waits longer than that stops the download.
	timer := time.AfterFunc(timeout, func() { stall(errStalled) })
	buffer := make([]byte, chunk)
	var sinceCheck int64
	for {
		n, err := response.Body.Read(buffer)
		timer.Reset(timeout)
		if n > 0 {
			if _, err := out.Write(buffer[:n]); err != nil {
				return err
			}
			m.mu.Lock()
			j.Received += int64(n)
			m.mu.Unlock()
			if sinceCheck += int64(n); sinceCheck >= m.checkEvery {
				sinceCheck = 0
				if m.room() == 0 {
					return stopped("The disk is nearly full")
				}
				if _, err := m.disk.Lstat(partial); err != nil { // its folder was renamed, moved or trashed
					return stopped("The folder it was going into moved")
				}
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			if cause := context.Cause(ctx); cause != nil && cause != context.Canceled {
				return cause
			}
			return err
		}
	}
	timer.Stop()
	if err := out.Close(); err != nil {
		return err
	}

	m.disk.NameLock.Lock()
	if _, err := m.disk.Lstat(partial); err != nil {
		m.disk.NameLock.Unlock()
		return stopped("The folder it was going into moved")
	}
	name = m.disk.FreeName(j.Folder, name, false)
	path := storage.Child(j.Folder, name)
	err = m.disk.Rename(partial, path)
	m.disk.NameLock.Unlock()
	if err != nil {
		return err
	}
	m.mu.Lock()
	j.Name, j.Path = name, path
	m.mu.Unlock()
	return m.db.Update(func(tx *db.Tx) error {
		if err := tx.Record(path); err != nil {
			return err
		}
		if err := tx.DropTags(path); err != nil {
			return err
		}
		return tx.AddTags([]string{path}, j.tags)
	})
}

// room is how many bytes a download may still write to the disk.
func (m *Manager) room() uint64 {
	_, _, free, err := m.disk.Usage()
	if err != nil || free < m.keepFree {
		return 0
	}
	return free - m.keepFree
}

// failure is why j failed, in words for the list.
func (m *Manager) failure(j *job, err error) string {
	stop, isStopped := errors.AsType[stopped](err)
	urlError, beforeAnswer := errors.AsType[*url.Error](err)
	switch {
	case isStopped:
		return string(stop)
	case errors.Is(err, errStalled):
		return errStalled.Error()
	case errors.Is(err, io.ErrUnexpectedEOF):
		return "The connection closed before the end"
	case beforeAnswer:
		if problem := storage.Problem(err); problem != "" {
			return "Couldn't reach it: " + problem
		}
		if dnsError, ok := errors.AsType[*net.DNSError](err); ok {
			return "Couldn't reach it: " + dnsError.Err
		}
		if urlError.Timeout() {
			return "Couldn't reach it: timed out"
		}
		return "Couldn't reach it: " + urlError.Err.Error()
	case storage.Problem(err) != "":
		return storage.Problem(err)
	}
	slog.Error("A download failed", "url", j.URL, "err", err)
	return "Something went wrong on the server"
}
