package server

import (
	"net/http"

	"github.com/cavecomputing/binny/internal/downloader"
)

func (s *Server) listDownloads(w http.ResponseWriter, r *http.Request) error {
	return reply(w, http.StatusOK, map[string][]downloader.Status{"downloads": s.downloads.List()})
}

// startDownload is {url, folder, tags}: fetch url into folder on the server, tagged with tags.
func (s *Server) startDownload(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		URL, Folder string
		Tags        []string
	}
	if err := decode(r, &body); err != nil {
		return err
	}
	link, err := downloader.ParseURL(body.URL)
	if err != nil {
		return fail(http.StatusBadRequest, "%s", err)
	}
	folder, err := s.folderArg(body.Folder)
	if err != nil {
		return err
	}
	tags, err := tagsArg(body.Tags)
	if err != nil {
		return err
	}
	return reply(w, http.StatusCreated, s.downloads.Start(link, folder, tags))
}

func (s *Server) cancelDownload(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if !s.downloads.Cancel(id) {
		return fail(http.StatusNotFound, "That download has already finished")
	}
	return reply(w, http.StatusOK, map[string]string{"cancelled": id})
}

func (s *Server) clearDownloads(w http.ResponseWriter, r *http.Request) error {
	return reply(w, http.StatusOK, map[string]int{"cleared": s.downloads.Clear()})
}
