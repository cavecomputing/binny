package server

import (
	"database/sql"
	"io"
	"io/fs"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"syscall"
	"testing"
	"testing/iotest"
	"time"

	_ "modernc.org/sqlite"
)

// filesRows is the index's (path, parent) pairs, read from the database file itself.
func filesRows(t *testing.T, a *app) [][2]string {
	t.Helper()
	conn, err := sql.Open("sqlite", "file:"+filepath.Join(a.dataDir, "binny.db")+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	result, err := conn.Query(`SELECT path, parent FROM entries ORDER BY path`)
	if err != nil {
		t.Fatal(err)
	}
	defer result.Close()
	found := [][2]string{}
	for result.Next() {
		var row [2]string
		if err := result.Scan(&row[0], &row[1]); err != nil {
			t.Fatal(err)
		}
		found = append(found, row)
	}
	if err := result.Err(); err != nil {
		t.Fatal(err)
	}
	return found
}

func filesWantRows(t *testing.T, a *app, want [][2]string) {
	t.Helper()
	if got := filesRows(t, a); !reflect.DeepEqual(got, want) {
		t.Fatalf("entries rows = %v, want %v", got, want)
	}
}

func filesWantJSON(t *testing.T, response *httptest.ResponseRecorder, want string) {
	t.Helper()
	if got := jsonOf(t, response); got != want {
		t.Fatalf("answered %s, want %s", got, want)
	}
}

func filesWantNames(t *testing.T, dir string, want ...string) {
	t.Helper()
	if want == nil {
		want = []string{}
	}
	if got := names(t, dir); !reflect.DeepEqual(got, want) {
		t.Fatalf("%s holds %q, want %q", dir, got, want)
	}
}

func TestUploadWritesTheFileAndListsIt(t *testing.T) {
	a := newApp(t)
	response := a.upload("notes.txt", "hello", "")
	if response.Code != http.StatusCreated {
		t.Fatalf("upload answered %d, want 201", response.Code)
	}
	filesWantJSON(t, response, `{"path":"notes.txt"}`)
	if got := a.read("notes.txt"); got != "hello" {
		t.Fatalf("notes.txt holds %q, want %q", got, "hello")
	}
	item := a.listing("")["notes.txt"]
	if item.Path != "notes.txt" || item.Size != 5 || item.Kind != "doc" || item.IsDir {
		t.Fatalf("listed %+v, want path notes.txt, size 5, kind doc, not a folder", item)
	}
}

func TestUploadNeverOverwrites(t *testing.T) {
	a := newApp(t)
	a.upload("a.txt", "one", "")
	filesWantJSON(t, a.upload("a.txt", "two", ""), `{"path":"a (1).txt"}`)
	if got := a.read("a.txt"); got != "one" {
		t.Fatalf("a.txt holds %q, want %q", got, "one")
	}
	if got := a.read("a (1).txt"); got != "two" {
		t.Fatalf("a (1).txt holds %q, want %q", got, "two")
	}
}

func TestUploadCreatesTheSubfoldersOfADroppedFolder(t *testing.T) {
	a := newApp(t)
	filesWantJSON(t, a.upload("trip/day 1/a.jpg", "hello", ""), `{"path":"trip/day 1/a.jpg"}`)
	if response := a.upload("trip/day 1/b.jpg", "hello", ""); response.Code != http.StatusCreated {
		t.Fatalf("second upload answered %d, want 201", response.Code)
	}
	filesWantRows(t, a, [][2]string{{"trip", ""}, {"trip/day 1", "trip"}, {"trip/day 1/a.jpg", "trip/day 1"},
		{"trip/day 1/b.jpg", "trip/day 1"}})
	trip := a.listing("")["trip"]
	if !trip.IsDir || itemCount(trip) != 1 || trip.Size != 10 {
		t.Fatalf("trip listed as %+v, want a folder with 1 item and 10 bytes", trip)
	}
}

func TestUploadIntoAFolderThatIsAFileIsRefused(t *testing.T) {
	a := newApp(t)
	a.upload("trip", "hello", "")
	if got := a.upload("trip/a.jpg", "hello", "").Code; got != http.StatusConflict {
		t.Fatalf("upload answered %d, want 409", got)
	}
}

func TestUploadRefusesBadTargets(t *testing.T) {
	for _, target := range []struct{ folder, path string }{
		{"", "../escape.txt"}, {"", ".trash/x.txt"}, {"", ".hidden"}, {"", ""}, {"..", "escape.txt"},
		{"missing", "a.txt"},
	} {
		t.Run(target.folder+"|"+target.path, func(t *testing.T) {
			a := newApp(t)
			if got := a.upload(target.path, "hello", target.folder).Code; got != 400 && got != 404 {
				t.Fatalf("upload answered %d, want 400 or 404", got)
			}
			if _, err := os.Lstat(filepath.Join(a.dataDir, "escape.txt")); err == nil {
				t.Fatal("escape.txt was written outside data/files")
			}
			filesWantNames(t, a.files)
		})
	}
}

func TestAnUploadCutShortLeavesNothingBehind(t *testing.T) {
	a := newApp(t)
	request := httptest.NewRequest("PUT", "/api/upload?"+url.Values{"path": {"big.iso"}}.Encode(),
		io.MultiReader(strings.NewReader("abc"), iotest.ErrReader(io.ErrUnexpectedEOF)))
	request.Header.Set("Cookie", a.cookie)
	response := httptest.NewRecorder()
	a.handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("upload answered %d, want 400", response.Code)
	}
	filesWantNames(t, a.files)
}

func TestListingDeletesPartialFilesLeftBehind(t *testing.T) {
	a := newApp(t)
	if err := os.Mkdir(filepath.Join(a.files, "docs"), 0o777); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(a.files, "docs", ".binny-"+strings.Repeat("a", 32)+".part")
	fresh := filepath.Join(a.files, ".binny-"+strings.Repeat("b", 32)+".part")
	other := filepath.Join(a.files, ".notes.part")
	for _, path := range []string{stale, fresh, other} {
		if err := os.WriteFile(path, []byte("half"), 0o666); err != nil {
			t.Fatal(err)
		}
	}
	longAgo := time.Now().Add(-24*time.Hour - time.Minute)
	for _, path := range []string{stale, other} {
		if err := os.Chtimes(path, longAgo, longAgo); err != nil {
			t.Fatal(err)
		}
	}
	a.listing("")
	a.listing("docs")
	exists := func(path string) bool { _, err := os.Lstat(path); return err == nil }
	if got := [3]bool{exists(stale), exists(fresh), exists(other)}; got != [3]bool{false, true, true} {
		t.Fatalf("stale, fresh, other exist = %v, want [false true true]", got)
	}
}

func TestListingPicksUpChangesMadeOutsideTheApp(t *testing.T) {
	a := newApp(t)
	a.write("a.txt", "one")
	if err := os.Mkdir(filepath.Join(a.files, "b"), 0o777); err != nil {
		t.Fatal(err)
	}
	if got := slices.Sorted(maps.Keys(a.listing(""))); !reflect.DeepEqual(got, []string{"a.txt", "b"}) {
		t.Fatalf("listed %q, want [a.txt b]", got)
	}

	if err := os.Remove(filepath.Join(a.files, "a.txt")); err != nil {
		t.Fatal(err)
	}
	a.write("c.txt", "three")
	a.write("b/inner.txt", "in")
	items := a.listing("")
	if got := slices.Sorted(maps.Keys(items)); !reflect.DeepEqual(got, []string{"b", "c.txt"}) {
		t.Fatalf("listed %q, want [b c.txt]", got)
	}
	if got := items["c.txt"].Size; got != 5 {
		t.Fatalf("c.txt size = %d, want 5", got)
	}
	// b's modified time moved, so b was synced too.
	if b := items["b"]; itemCount(b) != 1 || b.Size != 2 {
		t.Fatalf("b has %d items and %d bytes, want 1 and 2", itemCount(b), b.Size)
	}
}

func TestListingIndexesAFolderAddedOutsideAllTheWayDown(t *testing.T) {
	a := newApp(t)
	a.write("new/deep/x.txt", "12345")
	if n := a.listing("")["new"]; itemCount(n) != 1 || n.Size != 5 {
		t.Fatalf("new has %d items and %d bytes, want 1 and 5", itemCount(n), n.Size)
	}
}

func TestListingHidesDotNamesAndSymlinks(t *testing.T) {
	a := newApp(t)
	a.write("a.txt", "x")
	a.write(".secret", "x")
	if err := os.Mkdir(filepath.Join(a.files, ".trash"), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(a.files, "a.txt"), filepath.Join(a.files, "link.txt")); err != nil {
		t.Fatal(err)
	}
	if got := slices.Sorted(maps.Keys(a.listing(""))); !reflect.DeepEqual(got, []string{"a.txt"}) {
		t.Fatalf("listed %q, want [a.txt]", got)
	}
}

func TestListingAMissingFolderIs404(t *testing.T) {
	a := newApp(t)
	response := a.do("GET", "/api/list?folder=nope", nil)
	if response.Code != http.StatusNotFound {
		t.Fatalf("listing answered %d, want 404", response.Code)
	}
	filesWantJSON(t, response, `{"error":"That folder no longer exists"}`)
}

func TestFolderTotalsCountEverythingBelow(t *testing.T) {
	a := newApp(t)
	a.upload("a/b/c.bin", strings.Repeat("x", 10), "")
	a.upload("a/d.bin", strings.Repeat("x", 5), "")
	if folder := a.listing("")["a"]; itemCount(folder) != 2 || folder.Size != 15 {
		t.Fatalf("a has %d items and %d bytes, want 2 and 15", itemCount(folder), folder.Size)
	}
}

func TestRenameAFile(t *testing.T) {
	a := newApp(t)
	a.upload("a.txt", "hello", "")
	filesWantJSON(t, a.do("POST", "/api/rename", map[string]string{"path": "a.txt", "name": " b.txt "}),
		`{"path":"b.txt"}`)
	filesWantNames(t, a.files, "b.txt")
	filesWantRows(t, a, [][2]string{{"b.txt", ""}})
}

func TestRenameRefusesATakenName(t *testing.T) {
	a := newApp(t)
	a.upload("a.txt", "a", "")
	a.upload("b.txt", "b", "")
	response := a.do("POST", "/api/rename", map[string]string{"path": "a.txt", "name": "b.txt"})
	if response.Code != http.StatusConflict {
		t.Fatalf("rename answered %d, want 409", response.Code)
	}
	filesWantJSON(t, response, `{"error":"There is already a \"b.txt\" here"}`)
	if a.read("a.txt") != "a" || a.read("b.txt") != "b" {
		t.Fatalf("a.txt holds %q and b.txt %q, want a and b", a.read("a.txt"), a.read("b.txt"))
	}
}

func TestRenameAFolderRekeysEverythingBelow(t *testing.T) {
	a := newApp(t)
	a.upload("old/sub/x.txt", "hello", "")
	a.upload("older.txt", "hello", "") // sorts between "old" and "old/..." without a slash: must not move
	a.do("POST", "/api/rename", map[string]string{"path": "old", "name": "new"})
	filesWantRows(t, a, [][2]string{{"new", ""}, {"new/sub", "new"}, {"new/sub/x.txt", "new/sub"}, {"older.txt", ""}})
	if _, err := os.Stat(filepath.Join(a.files, "new", "sub", "x.txt")); err != nil {
		t.Fatalf("new/sub/x.txt: %v", err)
	}
}

func TestRenameRefusesBadRequests(t *testing.T) {
	for _, body := range []map[string]string{
		{"path": "", "name": "x"}, {"path": "a.txt", "name": ".x"}, {"path": "a.txt", "name": "a/b"}, {"path": "a.txt"},
	} {
		t.Run(body["path"]+"|"+body["name"], func(t *testing.T) {
			a := newApp(t)
			a.upload("a.txt", "hello", "")
			if got := a.do("POST", "/api/rename", body).Code; got != http.StatusBadRequest {
				t.Fatalf("rename answered %d, want 400", got)
			}
			filesWantNames(t, a.files, "a.txt")
		})
	}
}

func TestMoveIntoAFolder(t *testing.T) {
	a := newApp(t)
	a.upload("a.txt", "hello", "")
	a.upload("b/inner.txt", "hello", "")
	a.upload("dest/keep.txt", "hello", "")
	filesWantJSON(t, a.do("POST", "/api/move", map[string]any{"paths": []string{"a.txt", "b"}, "to": "dest"}),
		`{"moved":2,"renamed":0}`)
	filesWantNames(t, filepath.Join(a.files, "dest"), "a.txt", "b", "keep.txt")
	filesWantRows(t, a, [][2]string{{"dest", ""}, {"dest/a.txt", "dest"}, {"dest/b", "dest"},
		{"dest/b/inner.txt", "dest/b"}, {"dest/keep.txt", "dest"}})
}

func TestMoveNumbersATakenName(t *testing.T) {
	a := newApp(t)
	a.upload("a.txt", "top", "")
	a.upload("dest/a.txt", "inner", "")
	filesWantJSON(t, a.do("POST", "/api/move", map[string]any{"paths": []string{"a.txt"}, "to": "dest"}),
		`{"moved":1,"renamed":1}`)
	if got := a.read("dest/a.txt"); got != "inner" {
		t.Fatalf("dest/a.txt holds %q, want inner", got)
	}
	if got := a.read("dest/a (1).txt"); got != "top" {
		t.Fatalf("dest/a (1).txt holds %q, want top", got)
	}
}

func TestMoveIntoItselfIsRefused(t *testing.T) {
	a := newApp(t)
	a.upload("a/b/x.txt", "hello", "")
	a.upload("c.txt", "hello", "")
	response := a.do("POST", "/api/move", map[string]any{"paths": []string{"c.txt", "a"}, "to": "a/b"})
	if response.Code != http.StatusBadRequest {
		t.Fatalf("move answered %d, want 400", response.Code)
	}
	filesWantJSON(t, response, `{"error":"\"a\" can't go inside itself"}`)
	if _, err := os.Stat(filepath.Join(a.files, "c.txt")); err != nil {
		t.Fatalf("c.txt: %v", err)
	}
}

func TestMoveToWhereItAlreadyIsDoesNothing(t *testing.T) {
	a := newApp(t)
	a.upload("a.txt", "hello", "")
	filesWantJSON(t, a.do("POST", "/api/move", map[string]any{"paths": []string{"a.txt"}, "to": ""}),
		`{"moved":0,"renamed":0}`)
}

func TestMoveOfAMissingItemIs404(t *testing.T) {
	a := newApp(t)
	response := a.do("POST", "/api/move", map[string]any{"paths": []string{"ghost.txt"}, "to": ""})
	if response.Code != http.StatusNotFound {
		t.Fatalf("move answered %d, want 404", response.Code)
	}
	filesWantJSON(t, response, `{"error":"\"ghost.txt\" no longer exists"}`)
}

func TestDiskErrorsSayWhatFailed(t *testing.T) {
	for _, refusal := range []error{
		&os.LinkError{Op: "rename", Old: "b.txt", New: "dest/b.txt", Err: syscall.EACCES},
		&fs.PathError{Op: "open", Path: "b.txt", Err: syscall.EACCES},
	} {
		t.Run(refusal.Error(), func(t *testing.T) {
			handler := (&Server{}).handle(func(http.ResponseWriter, *http.Request) error { return refusal })
			response := httptest.NewRecorder()
			handler(response, httptest.NewRequest("POST", "/api/move", nil))
			if response.Code != http.StatusInternalServerError {
				t.Fatalf("answered %d, want 500", response.Code)
			}
			filesWantJSON(t, response, `{"error":"Permission denied (\"b.txt\")"}`)
		})
	}
}
