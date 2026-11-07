package server

import (
	"fmt"
	"mime"
	"net/http"
	"strings"

	"github.com/cavecomputing/binny/internal/archive"
	"github.com/cavecomputing/binny/internal/storage"
)

// shownAs is the type a stored file is sent as, and whether the browser may show it in the tab
// rather than download it. Only media, PDFs and text open in the tab. Text of every kind, HTML
// included, is sent as text/plain, so a stored page can never run as part of Binny.
func shownAs(name string) (contentType string, inline bool) {
	contentType = storage.ContentType(name)
	if strings.HasPrefix(contentType, "text/") || storage.KindOf(name, false) == "code" {
		return "text/plain; charset=utf-8", true
	}
	for _, shown := range []string{"image/", "video/", "audio/", "application/pdf"} {
		if strings.HasPrefix(contentType, shown) {
			return contentType, true
		}
	}
	return contentType, false
}

// storedFile is a file, shown or downloaded (always downloaded with ?download), with range
// requests for seeking and resuming. A folder downloads as a zip.
func (s *Server) storedFile(w http.ResponseWriter, r *http.Request) error {
	rel, err := cleaned(r.PathValue("path"))
	if err != nil {
		return err
	}
	info, ok := s.disk.Plain(rel)
	if !ok {
		return fail(http.StatusNotFound, "Not found")
	}
	name := storage.NameOf(rel)
	if info.IsDir() {
		if name == "" {
			name = "files"
		}
		return s.sendZip(w, name, []archive.Entry{{Path: rel, Name: name}})
	}
	file, err := s.disk.Open(rel)
	if err != nil {
		return err
	}
	defer file.Close()
	contentType, inline := shownAs(name)
	disposition := "inline"
	if !inline || r.URL.Query().Has("download") {
		disposition = "attachment"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": name}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if contentType == "image/svg+xml" { // the one type shown in the tab that can carry script
		w.Header().Set("Content-Security-Policy", "sandbox")
	}
	// Revalidate every time, by mtime and size, so a file replaced under the same name is sent again.
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("ETag", fmt.Sprintf(`"%x-%x"`, info.ModTime().UnixNano(), info.Size()))
	http.ServeContent(w, r, name, info.ModTime(), file)
	return nil
}

// zip is several files and folders as one zip, named after the folder they're in ("files" when a
// search picked them from several). It is a plain form post, so the browser saves the response
// as it arrives instead of the page holding it.
func (s *Server) zip(w http.ResponseWriter, r *http.Request) error {
	if err := r.ParseForm(); err != nil {
		return fail(http.StatusBadRequest, "%s", err)
	}
	items, err := s.itemsArg(r.PostForm["paths"], false)
	if err != nil {
		return err
	}
	folders := map[string]bool{}
	var entries []archive.Entry
	taken := map[string]bool{}
	for _, it := range items {
		folders[storage.ParentOf(it.path)] = true
		// Two picked from different folders can share a name: number the second.
		for name := range storage.Numbered(storage.NameOf(it.path), it.info.IsDir()) {
			if !taken[strings.ToLower(name)] {
				taken[strings.ToLower(name)] = true
				entries = append(entries, archive.Entry{Path: it.path, Name: name})
				break
			}
		}
	}
	zipName := "files"
	if len(folders) == 1 && storage.ParentOf(items[0].path) != "" {
		zipName = storage.NameOf(storage.ParentOf(items[0].path))
	}
	return s.sendZip(w, zipName, entries)
}

// sendZip streams entries as name.zip. Once it has started, a failure can only cut the download
// short, so the browser sees it fail rather than save a broken zip.
func (s *Server) sendZip(w http.ResponseWriter, name string, entries []archive.Entry) error {
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name + ".zip"}))
	if err := archive.Write(w, s.disk, entries); err != nil {
		panic(http.ErrAbortHandler)
	}
	return nil
}
