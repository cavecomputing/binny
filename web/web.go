// Package web holds the page's templates and static files, built into the binary.
package web

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"html/template"
	"io/fs"
	"mime"
	"net/http"
)

//go:embed static templates
var files embed.FS

// Page is the template of one page, laid out by base.html.
func Page(name string) *template.Template {
	return template.Must(template.ParseFS(files, "templates/base.html", "templates/"+name))
}

// Static serves the static files under /static/, with an ETag so browsers revalidate rather than
// fetch them again.
func Static() http.Handler {
	mime.AddExtensionType(".webmanifest", "application/manifest+json")
	static, _ := fs.Sub(files, "static")
	etags := map[string]string{}
	fs.WalkDir(static, ".", func(path string, entry fs.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			content, _ := fs.ReadFile(static, path)
			sum := sha256.Sum256(content)
			etags["/static/"+path] = `"` + hex.EncodeToString(sum[:8]) + `"`
		}
		return err
	})
	server := http.StripPrefix("/static/", http.FileServerFS(static))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		etag, ok := etags[r.URL.Path]
		if !ok {
			http.NotFound(w, r) // folders too, rather than a listing
			return
		}
		w.Header().Set("ETag", etag)
		server.ServeHTTP(w, r)
	})
}
