// Package server is Binny's HTTP side: the page, the stored files and the JSON API under /api.
package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/cavecomputing/binny/internal/auth"
	"github.com/cavecomputing/binny/internal/db"
	"github.com/cavecomputing/binny/internal/downloader"
	"github.com/cavecomputing/binny/internal/storage"
	"github.com/cavecomputing/binny/web"
)

type Server struct {
	disk      *storage.Disk
	db        *db.DB
	downloads *downloader.Manager
	page      *template.Template
}

// New is the whole app as one handler, every route but the sign-in page and the static files
// behind the login.
func New(disk *storage.Disk, store *db.DB, login *auth.Auth, downloads *downloader.Manager) http.Handler {
	s := &Server{disk: disk, db: store, downloads: downloads, page: web.Page("index.html")}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /login", login.LoginPage)
	mux.HandleFunc("POST /login", login.Login)
	mux.HandleFunc("POST /logout", login.Logout)
	mux.Handle("GET /static/", web.Static())

	mux.HandleFunc("GET /{$}", s.index)
	mux.HandleFunc("GET /files/{path...}", s.handle(s.storedFile))
	mux.HandleFunc("POST /zip", s.handle(s.zip))

	mux.HandleFunc("GET /api/list", s.handle(s.list))
	mux.HandleFunc("PUT /api/upload", s.handle(s.upload))
	mux.HandleFunc("POST /api/rename", s.handle(s.rename))
	mux.HandleFunc("POST /api/move", s.handle(s.move))

	mux.HandleFunc("GET /api/folders", s.handle(s.folderTree))
	mux.HandleFunc("GET /api/folders/all", s.handle(s.allFolders))
	mux.HandleFunc("POST /api/folders", s.handle(s.createFolder))

	mux.HandleFunc("GET /api/trash", s.handle(s.listTrash))
	mux.HandleFunc("POST /api/trash", s.handle(s.trash))
	mux.HandleFunc("POST /api/trash/restore", s.handle(s.restore))
	mux.HandleFunc("POST /api/trash/delete", s.handle(s.deleteForever))
	mux.HandleFunc("POST /api/trash/empty", s.handle(s.emptyTrash))

	mux.HandleFunc("GET /api/tags", s.handle(s.listTags))
	mux.HandleFunc("POST /api/tags", s.handle(s.tagItems))
	mux.HandleFunc("POST /api/tags/rename", s.handle(s.renameTag))
	mux.HandleFunc("POST /api/tags/delete", s.handle(s.deleteTag))
	mux.HandleFunc("GET /api/search", s.handle(s.search))

	mux.HandleFunc("GET /api/downloads", s.handle(s.listDownloads))
	mux.HandleFunc("POST /api/downloads", s.handle(s.startDownload))
	mux.HandleFunc("POST /api/downloads/{id}/cancel", s.handle(s.cancelDownload))
	mux.HandleFunc("POST /api/downloads/clear", s.handle(s.clearDownloads))

	// Writes from another site's page in the same browser are refused.
	crossOrigin := http.NewCrossOriginProtection()
	crossOrigin.SetDenyHandler(s.handle(func(http.ResponseWriter, *http.Request) error {
		return fail(http.StatusForbidden, "Cross-site request blocked")
	}))
	guarded := crossOrigin.Handler(login.Require(mux))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY") // no other page, a sibling app's included, may frame this one
		guarded.ServeHTTP(w, r)
	})
}

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	s.page.Execute(w, nil)
}

// httpError is a failure the client is told about, with its status.
type httpError struct {
	status  int
	message string
}

func (e *httpError) Error() string { return e.message }

func fail(status int, format string, args ...any) error {
	return &httpError{status, fmt.Sprintf(format, args...)}
}

// handle adapts a handler that returns an error: an httpError's message goes to the client with
// its status, a disk error says what the disk said, and anything else is a 500. API errors are
// {"error": message}.
func (s *Server) handle(handler func(w http.ResponseWriter, r *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := handler(w, r)
		if err == nil {
			return
		}
		status, message := http.StatusInternalServerError, "Something went wrong on the server"
		known, isKnown := errors.AsType[*httpError](err)
		switch {
		case isKnown:
			status, message = known.status, known.message
		case storage.Problem(err) != "":
			slog.Warn("The disk refused", "route", r.Pattern, "err", err)
			message = storage.Problem(err)
			if linkError, ok := errors.AsType[*os.LinkError](err); ok {
				message += fmt.Sprintf(` ("%s")`, path.Base(linkError.Old))
			} else if pathError, ok := errors.AsType[*fs.PathError](err); ok {
				message += fmt.Sprintf(` ("%s")`, path.Base(pathError.Path))
			}
		default:
			slog.Error("Unhandled error", "route", r.Pattern, "err", err)
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			reply(w, status, map[string]string{"error": message})
		} else {
			http.Error(w, message, status)
		}
	}
}

// reply sends value as JSON with status. It returns nil, so a handler can end with it.
func reply(w http.ResponseWriter, status int, value any) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
	return nil
}
