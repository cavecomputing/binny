package server

import (
	"database/sql"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// trashPaths trashes paths and returns their names in the trash.
func (a *app) trashPaths(paths ...string) []string {
	a.t.Helper()
	response := a.do("POST", "/api/trash", map[string][]string{"paths": paths})
	if response.Code != http.StatusOK {
		a.t.Fatalf("trashing %q answered %d: %s", paths, response.Code, response.Body)
	}
	var body struct{ Names []string }
	decodeInto(a.t, response, &body)
	return body.Names
}

// trashItems is GET /api/trash's items.
func (a *app) trashItems() []trashed {
	a.t.Helper()
	var body struct{ Items []trashed }
	decodeInto(a.t, a.do("GET", "/api/trash", nil), &body)
	return body.Items
}

// trashDB is a read-only connection to the app's database.
func (a *app) trashDB() *sql.DB {
	a.t.Helper()
	conn, err := sql.Open("sqlite", "file:"+filepath.Join(a.dataDir, "binny.db")+"?mode=ro")
	if err != nil {
		a.t.Fatal(err)
	}
	a.t.Cleanup(func() { conn.Close() })
	return conn
}

// trashIndexed is the entries rows whose path starts with prefix, sorted.
func (a *app) trashIndexed(prefix string) []string {
	a.t.Helper()
	rows, err := a.trashDB().Query("SELECT path FROM entries WHERE path LIKE ? || '%' ORDER BY path", prefix)
	if err != nil {
		a.t.Fatal(err)
	}
	defer rows.Close()
	paths := []string{}
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			a.t.Fatal(err)
		}
		paths = append(paths, path)
	}
	if err := rows.Err(); err != nil {
		a.t.Fatal(err)
	}
	return paths
}

func trashFolder(a *app) string { return filepath.Join(a.files, ".trash") }

func TestTrashMovesAFileIntoTheHiddenTrash(t *testing.T) {
	a := newApp(t)
	a.upload("docs/a.txt", "12345", "")
	names := a.trashPaths("docs/a.txt")
	if len(names) != 1 {
		t.Fatalf("names = %q, want one", names)
	}
	name := names[0]
	if !strings.HasSuffix(name, "_a.txt") {
		t.Errorf("name = %q, want it to end in _a.txt", name)
	}
	if _, err := os.Stat(filepath.Join(a.files, "docs", "a.txt")); !os.IsNotExist(err) {
		t.Errorf("docs/a.txt is still there (%v)", err)
	}
	if got, _ := os.ReadFile(filepath.Join(trashFolder(a), name)); string(got) != "12345" {
		t.Errorf("trashed content = %q, want %q", got, "12345")
	}
	if got := a.listing("docs"); len(got) != 0 {
		t.Errorf("docs lists %v, want nothing", got)
	}
	items := a.trashItems()
	if len(items) != 1 {
		t.Fatalf("trash items = %+v, want one", items)
	}
	item := items[0]
	if item.Name != name || item.Original != "docs/a.txt" || item.Size != 5 || item.IsDir || item.Kind != "doc" {
		t.Errorf("trash item = %+v, want name %q, original docs/a.txt, size 5, a file, kind doc", item, name)
	}
	var folders struct {
		TrashItems int `json:"trash_items"`
	}
	decodeInto(t, a.do("GET", "/api/folders", nil), &folders)
	if folders.TrashItems != 1 {
		t.Errorf("trash_items = %d, want 1", folders.TrashItems)
	}
}

func TestANameAsLongAsTheDiskAllowsTrashesAndRestores(t *testing.T) {
	a := newApp(t)
	name := strings.Repeat("a", 251) + ".txt"
	a.upload(name, "long", "")
	names := a.trashPaths(name)
	items := a.trashItems()
	if len(items) != 1 || items[0].Original != name || items[0].Kind != "doc" {
		t.Fatalf("trash items = %+v, want one with original %q and kind doc", items, name)
	}
	a.upload(name, "taken", "")
	restoredName := strings.Repeat("a", 247) + " (1).txt"
	if got, want := jsonOf(t, a.do("POST", "/api/trash/restore", map[string][]string{"names": names})), `{"restored":["`+restoredName+`"]}`; got != want {
		t.Errorf("restore answered %s, want %s", got, want)
	}
	if got := a.read(restoredName); got != "long" {
		t.Errorf("restored content = %q, want %q", got, "long")
	}
}

func TestTrashingAFolderDropsItsRowsAndRemembersItsSize(t *testing.T) {
	a := newApp(t)
	a.upload("trip/day/a.jpg", "123", "")
	a.upload("trip/b.jpg", "45", "")
	a.trashPaths("trip")
	if got := a.trashIndexed("trip"); len(got) != 0 {
		t.Errorf("rows under trip = %q, want none", got)
	}
	if got := a.listing(""); len(got) != 0 {
		t.Errorf("top folder lists %v, want nothing", got)
	}
	items := a.trashItems()
	if len(items) != 1 {
		t.Fatalf("trash items = %+v, want one", items)
	}
	item := items[0]
	if item.Original != "trip" || item.Size != 5 || !item.IsDir || item.Kind != "folder" {
		t.Errorf("trash item = %+v, want original trip, size 5, a folder, kind folder", item)
	}
}

func TestRestorePutsItBackWithItsRows(t *testing.T) {
	a := newApp(t)
	a.upload("trip/day/a.jpg", "123", "")
	names := a.trashPaths("trip")
	if got := jsonOf(t, a.do("POST", "/api/trash/restore", map[string][]string{"names": names})); got != `{"restored":["trip"]}` {
		t.Errorf("restore answered %s, want {\"restored\":[\"trip\"]}", got)
	}
	if got := a.read("trip/day/a.jpg"); got != "123" {
		t.Errorf("restored content = %q, want %q", got, "123")
	}
	if got, want := a.trashIndexed("trip"), []string{"trip", "trip/day", "trip/day/a.jpg"}; !slices.Equal(got, want) {
		t.Errorf("rows under trip = %q, want %q", got, want)
	}
	if got := a.trashItems(); len(got) != 0 {
		t.Errorf("trash items = %+v, want none", got)
	}
}

func TestRestoreKeepsBothWhenTheNameIsTaken(t *testing.T) {
	a := newApp(t)
	a.upload("a.txt", "old", "")
	names := a.trashPaths("a.txt")
	a.upload("a.txt", "new", "")
	if got := jsonOf(t, a.do("POST", "/api/trash/restore", map[string][]string{"names": names})); got != `{"restored":["a (1).txt"]}` {
		t.Errorf("restore answered %s, want {\"restored\":[\"a (1).txt\"]}", got)
	}
	if got := a.read("a.txt"); got != "new" {
		t.Errorf("a.txt = %q, want %q", got, "new")
	}
	if got := a.read("a (1).txt"); got != "old" {
		t.Errorf("a (1).txt = %q, want %q", got, "old")
	}
}

func TestRestoreMakesTheFolderItWasInAgain(t *testing.T) {
	a := newApp(t)
	a.upload("gone/deeper/a.txt", "hello", "")
	names := a.trashPaths("gone/deeper/a.txt")
	a.trashPaths("gone")
	a.do("POST", "/api/trash/restore", map[string][]string{"names": names})
	if _, err := os.Stat(filepath.Join(a.files, "gone", "deeper", "a.txt")); err != nil {
		t.Errorf("gone/deeper/a.txt isn't back: %v", err)
	}
	if got, want := a.trashIndexed("gone"), []string{"gone", "gone/deeper", "gone/deeper/a.txt"}; !slices.Equal(got, want) {
		t.Errorf("rows under gone = %q, want %q", got, want)
	}
}

func TestRestoreRefusesWhenAFileIsWhereItsFolderWas(t *testing.T) {
	a := newApp(t)
	a.upload("docs/a.txt", "hello", "")
	names := a.trashPaths("docs/a.txt")
	a.trashPaths("docs")
	a.upload("docs", "a file now", "")
	response := a.do("POST", "/api/trash/restore", map[string][]string{"names": names})
	if response.Code != http.StatusConflict {
		t.Errorf("restore answered %d, want 409", response.Code)
	}
	if got, want := jsonOf(t, response), `{"error":"Can't restore \"a.txt\": \"docs\" is a file, not a folder"}`; got != want {
		t.Errorf("restore answered %s, want %s", got, want)
	}
}

func TestThingsPutInTheTrashByHandShowAndRestoreToTheTop(t *testing.T) {
	a := newApp(t)
	if err := os.Mkdir(trashFolder(a), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(trashFolder(a), "stray.txt"), []byte("hi"), 0o666); err != nil {
		t.Fatal(err)
	}
	items := a.trashItems()
	if len(items) != 1 {
		t.Fatalf("trash items = %+v, want one", items)
	}
	if item := items[0]; item.Name != "stray.txt" || item.Original != "stray.txt" || item.Size != 2 {
		t.Errorf("trash item = %+v, want name and original stray.txt, size 2", item)
	}
	a.do("POST", "/api/trash/restore", map[string][]string{"names": {"stray.txt"}})
	if got := a.read("stray.txt"); got != "hi" {
		t.Errorf("stray.txt = %q, want %q", got, "hi")
	}
}

// "<digits>_<name>" reads as trashed through the app, but a name Binny won't accept in a folder
// (".hidden" would restore out of sight, ".." out of the folder) is no original name at all.
func TestAHandMadeTrashNameThatLooksTrashedButIsntRestoresAsItIs(t *testing.T) {
	a := newApp(t)
	if err := os.Mkdir(trashFolder(a), 0o777); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"1_.hidden", "2_..", "3_.trash", "4_"} {
		if err := os.WriteFile(filepath.Join(trashFolder(a), name), []byte("hi"), 0o666); err != nil {
			t.Fatal(err)
		}
	}
	for _, item := range a.trashItems() {
		if item.Original != item.Name {
			t.Errorf("%q is listed as coming from %q, want its own name", item.Name, item.Original)
		}
		if response := a.do("POST", "/api/trash/restore", map[string][]string{"names": {item.Name}}); response.Code != http.StatusOK {
			t.Errorf("restoring %q answered %d: %s", item.Name, response.Code, response.Body)
		}
	}
	if got := names(t, a.files); !slices.Equal(got, []string{".trash", "1_.hidden", "2_..", "3_.trash", "4_"}) {
		t.Errorf("after restoring, the top folder lists %q", got)
	}
}

// Restoring it would put a symlink, which listings hide, back among the files.
func TestASymlinkPutInTheTrashByHandIsHidden(t *testing.T) {
	a := newApp(t)
	if err := os.Mkdir(trashFolder(a), 0o777); err != nil {
		t.Fatal(err)
	}
	a.write("real.txt", "hi")
	if err := os.Symlink(filepath.Join(a.files, "real.txt"), filepath.Join(trashFolder(a), "link.txt")); err != nil {
		t.Fatal(err)
	}
	if got := a.trashItems(); len(got) != 0 {
		t.Errorf("trash items = %+v, want none", got)
	}
	if got := a.do("POST", "/api/trash/restore", map[string][]string{"names": {"link.txt"}}).Code; got != http.StatusNotFound {
		t.Errorf("restore answered %d, want 404", got)
	}
	if _, err := os.Lstat(filepath.Join(a.files, "link.txt")); !os.IsNotExist(err) {
		t.Errorf("link.txt was restored (%v)", err)
	}
}

func TestDeleteForeverRemovesOnlyWhatWasPicked(t *testing.T) {
	a := newApp(t)
	a.upload("a.txt", "hello", "")
	a.upload("folder/b.txt", "hello", "")
	trashedNames := a.trashPaths("a.txt", "folder")
	if got := jsonOf(t, a.do("POST", "/api/trash/delete", map[string][]string{"names": trashedNames[1:]})); got != `{"deleted":1}` {
		t.Errorf("delete answered %s, want {\"deleted\":1}", got)
	}
	var originals []string
	for _, item := range a.trashItems() {
		originals = append(originals, item.Original)
	}
	if want := []string{"a.txt"}; !slices.Equal(originals, want) {
		t.Errorf("trash originals = %q, want %q", originals, want)
	}
	if got, want := names(t, trashFolder(a)), trashedNames[:1]; !slices.Equal(got, want) {
		t.Errorf("in the trash on disk: %q, want %q", got, want)
	}
}

func TestEmptyRemovesEverything(t *testing.T) {
	a := newApp(t)
	a.upload("a.txt", "hello", "")
	a.upload("folder/b.txt", "hello", "")
	a.trashPaths("a.txt", "folder")
	if got := jsonOf(t, a.do("POST", "/api/trash/empty", nil)); got != `{"deleted":2}` {
		t.Errorf("empty answered %s, want {\"deleted\":2}", got)
	}
	if got := names(t, trashFolder(a)); len(got) != 0 {
		t.Errorf("in the trash on disk: %q, want nothing", got)
	}
	if got := a.trashItems(); len(got) != 0 {
		t.Errorf("trash items = %+v, want none", got)
	}
	var count int
	if err := a.trashDB().QueryRow("SELECT COUNT(*) FROM trash").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("trash rows = %d, want 0", count)
	}
}

func TestAFileWhereTheTrashGoesIsNamedInTheError(t *testing.T) {
	a := newApp(t)
	a.write(".trash", "not a folder")
	a.write("g.txt", "g")
	if got := jsonOf(t, a.do("POST", "/api/trash", map[string]any{"paths": []string{"g.txt"}})); !strings.Contains(got, `\".trash\"`) {
		t.Errorf("trashing answered %s, want an error naming .trash", got)
	}
}

func TestTheTopFolderAndTheTrashItselfCantBeTrashed(t *testing.T) {
	a := newApp(t)
	if err := os.Mkdir(trashFolder(a), 0o777); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"", ".trash"} {
		if got := a.do("POST", "/api/trash", map[string][]string{"paths": {path}}).Code; got != http.StatusBadRequest {
			t.Errorf("trashing %q answered %d, want 400", path, got)
		}
	}
}

func TestBadTrashNamesAreRefused(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
	}{{"../a.txt", 400}, {".secret", 400}, {"", 400}, {"nope", 404}} {
		t.Run(tc.name, func(t *testing.T) {
			a := newApp(t)
			for _, route := range []string{"/api/trash/restore", "/api/trash/delete"} {
				if got := a.do("POST", route, map[string][]string{"names": {tc.name}}).Code; got != tc.status {
					t.Errorf("%s of %q answered %d, want %d", route, tc.name, got, tc.status)
				}
			}
		})
	}
}
