package server

import (
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/cavecomputing/binny/internal/db"
)

func TestCreateAFolder(t *testing.T) {
	a := newApp(t)
	response := a.do("POST", "/api/folders", map[string]string{"parent": "", "name": "photos"})
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", response.Code, response.Body)
	}
	if got, want := jsonOf(t, response), `{"path":"photos"}`; got != want {
		t.Errorf("answer = %s, want %s", got, want)
	}
	if info, err := os.Stat(filepath.Join(a.files, "photos")); err != nil || !info.IsDir() {
		t.Errorf("photos is not a folder on disk (%v)", err)
	}
	nested := a.do("POST", "/api/folders", map[string]string{"parent": "photos", "name": "2026"})
	if got, want := jsonOf(t, nested), `{"path":"photos/2026"}`; got != want {
		t.Errorf("nested answer = %s, want %s", got, want)
	}
	if got := itemCount(a.listing("")["photos"]); got != 1 {
		t.Errorf("photos lists %d items, want 1", got)
	}
}

func TestCreateAFolderRefusesATakenName(t *testing.T) {
	a := newApp(t)
	a.write("notes", "a file")
	response := a.do("POST", "/api/folders", map[string]string{"parent": "", "name": "notes"})
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", response.Code)
	}
	if got, want := jsonOf(t, response), `{"error":"There is already a \"notes\" here"}`; got != want {
		t.Errorf("answer = %s, want %s", got, want)
	}
}

func TestCreateAFolderInAMissingParentIs404(t *testing.T) {
	a := newApp(t)
	response := a.do("POST", "/api/folders", map[string]string{"parent": "nope", "name": "x"})
	if response.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", response.Code)
	}
}

func TestTreeListsTheSubfoldersOfOpenFolders(t *testing.T) {
	a := newApp(t)
	a.upload("a/b/c/x.txt", "12345", "")
	a.upload("d/y.txt", "12", "")
	response := a.do("GET", "/api/folders?open=a&open=..%2Fetc", nil)
	var body struct {
		Tree      map[string][]db.Folder
		FilesSize int64 `json:"files_size"`
		Disk      struct{ Total, Used, Free uint64 }
	}
	decodeInto(t, response, &body)
	keys := []string{}
	for key := range body.Tree {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	if want := []string{"", "a"}; !slices.Equal(keys, want) {
		t.Errorf("tree has %q, want %q", keys, want)
	}
	top := body.Tree[""]
	slices.SortFunc(top, func(x, y db.Folder) int { return strings.Compare(x.Path, y.Path) })
	if want := []db.Folder{{Path: "a", Name: "a", HasChildren: true}, {Path: "d", Name: "d"}}; !reflect.DeepEqual(top, want) {
		t.Errorf("tree[\"\"] = %+v, want %+v", top, want)
	}
	if want := []db.Folder{{Path: "a/b", Name: "b", HasChildren: true}}; !reflect.DeepEqual(body.Tree["a"], want) {
		t.Errorf("tree[\"a\"] = %+v, want %+v", body.Tree["a"], want)
	}
	if body.FilesSize != 7 {
		t.Errorf("files_size = %d, want 7", body.FilesSize)
	}
	disk := body.Disk
	if !(disk.Total >= disk.Used && disk.Used > 0) {
		t.Errorf("disk total %d, used %d: want total >= used > 0", disk.Total, disk.Used)
	}
	if !(disk.Free > 0 && disk.Free <= disk.Total) {
		t.Errorf("disk free %d, total %d: want 0 < free <= total", disk.Free, disk.Total)
	}
}

func TestAllFoldersListsEveryFolder(t *testing.T) {
	a := newApp(t)
	for _, folder := range [][2]string{{"", "photos"}, {"photos", "2026"}, {"", "docs"}} {
		a.do("POST", "/api/folders", map[string]string{"parent": folder[0], "name": folder[1]})
	}
	a.upload("notes.txt", "hello", "")
	var body struct{ Folders []string }
	decodeInto(t, a.do("GET", "/api/folders/all", nil), &body)
	slices.Sort(body.Folders)
	if want := []string{"docs", "photos", "photos/2026"}; !slices.Equal(body.Folders, want) {
		t.Errorf("folders = %q, want %q", body.Folders, want)
	}
}
