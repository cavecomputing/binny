package server

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cavecomputing/binny/internal/db"
)

// tagChange posts a tag change and returns each item's tags afterwards.
func tagChange(a *app, paths, add, remove []string, rename ...[2]string) map[string][]string {
	a.t.Helper()
	pairs := [][]string{}
	for _, pair := range rename {
		pairs = append(pairs, []string{pair[0], pair[1]})
	}
	response := a.do("POST", "/api/tags", map[string]any{"paths": paths, "add": orNone(add), "remove": orNone(remove), "rename": pairs})
	if response.Code != http.StatusOK {
		a.t.Fatalf("tagging answered %d: %s", response.Code, response.Body)
	}
	var body struct{ Tags map[string][]string }
	decodeInto(a.t, response, &body)
	return body.Tags
}

func orNone(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

// tagCounts is the tags in use with how many items have each.
func tagCounts(a *app) map[string]int {
	a.t.Helper()
	var body struct{ Tags []db.TagCount }
	decodeInto(a.t, a.do("GET", "/api/tags", nil), &body)
	counts := map[string]int{}
	for _, row := range body.Tags {
		counts[row.Tag] = row.Count
	}
	return counts
}

// tagSearch is the paths a search finds, in order.
func tagSearch(a *app, query url.Values) []string {
	a.t.Helper()
	response := a.do("GET", "/api/search?"+query.Encode(), nil)
	if response.Code != http.StatusOK {
		a.t.Fatalf("searching %v answered %d: %s", query, response.Code, response.Body)
	}
	var body struct{ Items []db.Item }
	decodeInto(a.t, response, &body)
	paths := []string{}
	for _, item := range body.Items {
		paths = append(paths, item.Path)
	}
	return paths
}

// tagTrash trashes paths and returns their names in the trash.
func tagTrash(a *app, paths ...string) []string {
	a.t.Helper()
	var body struct{ Names []string }
	decodeInto(a.t, a.do("POST", "/api/trash", map[string]any{"paths": paths}), &body)
	return body.Names
}

func tagCheck(t *testing.T, what string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s: got %#v, want %#v", what, got, want)
	}
}

func TestTagsAreAddedLowercasedAndListedWithItems(t *testing.T) {
	a := newApp(t)
	a.upload("a.jpg", "hello", "")
	a.upload("b.jpg", "hello", "")
	tagCheck(t, "tagging", tagChange(a, []string{"a.jpg", "b.jpg"}, []string{"Family", " beach "}, nil),
		map[string][]string{"a.jpg": {"beach", "family"}, "b.jpg": {"beach", "family"}})
	tagCheck(t, "listed tags", a.listing("")["a.jpg"].Tags, []string{"beach", "family"})
	tagCheck(t, "counts", tagCounts(a), map[string]int{"beach": 2, "family": 2})
}

func TestTagsAreRemovedAndRenamedOnJustTheItemsThatHaveThem(t *testing.T) {
	a := newApp(t)
	for _, name := range []string{"a.jpg", "b.jpg", "c.jpg"} {
		a.upload(name, "hello", "")
	}
	tagChange(a, []string{"a.jpg", "b.jpg"}, []string{"beach"}, nil)
	tagChange(a, []string{"b.jpg"}, []string{"sea"}, nil)
	result := tagChange(a, []string{"a.jpg", "b.jpg", "c.jpg"}, nil, []string{"nope"}, [2]string{"beach", "sea"})
	tagCheck(t, "renaming", result, map[string][]string{"a.jpg": {"sea"}, "b.jpg": {"sea"}, "c.jpg": {}})
	tagCheck(t, "removing", tagChange(a, []string{"b.jpg"}, nil, []string{"sea"}), map[string][]string{"b.jpg": {}})
	tagCheck(t, "counts", tagCounts(a), map[string]int{"sea": 1})
}

func TestBadTagsAreRefused(t *testing.T) {
	for _, bad := range []any{"", "  ", "two words", "a,b", "old>new", "-x", "+x", "@x", strings.Repeat("x", 101), "tab\there", 7} {
		t.Run(fmt.Sprint(bad), func(t *testing.T) {
			a := newApp(t)
			a.upload("a.jpg", "hello", "")
			response := a.do("POST", "/api/tags", map[string]any{"paths": []string{"a.jpg"}, "add": []any{bad}})
			if response.Code != http.StatusBadRequest {
				t.Errorf("status: got %d, want 400", response.Code)
			}
			var body struct{ Error string }
			decodeInto(t, response, &body)
			if body.Error == "" {
				t.Errorf("no error message: %s", response.Body)
			}
		})
	}
}

func TestTaggingNeedsExistingItems(t *testing.T) {
	a := newApp(t)
	for _, c := range []struct {
		body any
		want int
	}{
		{map[string]any{"paths": []string{"missing.jpg"}, "add": []string{"x"}}, http.StatusNotFound},
		{map[string]any{"paths": []string{""}, "add": []string{"x"}}, http.StatusBadRequest},
		{map[string]any{"add": []string{"x"}}, http.StatusBadRequest},
	} {
		if got := a.do("POST", "/api/tags", c.body).Code; got != c.want {
			t.Errorf("tagging %v: got %d, want %d", c.body, got, c.want)
		}
	}
}

func TestAFolderAndSomethingInsideItAreBothTagged(t *testing.T) {
	a := newApp(t)
	a.upload("trip/a.jpg", "hello", "")
	tagCheck(t, "tagging", tagChange(a, []string{"trip", "trip/a.jpg"}, []string{"x"}, nil),
		map[string][]string{"trip": {"x"}, "trip/a.jpg": {"x"}})
}

func TestRenameEverywhereMergesIntoAnExistingTag(t *testing.T) {
	a := newApp(t)
	a.upload("a.jpg", "hello", "")
	a.upload("b.jpg", "hello", "")
	tagChange(a, []string{"a.jpg"}, []string{"beach", "sea"}, nil)
	tagChange(a, []string{"b.jpg"}, []string{"beach"}, nil)
	if got := jsonOf(t, a.do("POST", "/api/tags/rename", map[string]string{"old": "Beach", "new": "sea"})); got != `{"items":2,"tag":"sea"}` {
		t.Errorf("renaming: got %s", got)
	}
	tagCheck(t, "counts", tagCounts(a), map[string]int{"sea": 2})
	if got := a.do("POST", "/api/tags/rename", map[string]string{"old": "beach", "new": "x"}).Code; got != http.StatusNotFound {
		t.Errorf("renaming a tag nothing has: got %d, want 404", got)
	}
	if got := a.do("POST", "/api/tags/rename", map[string]string{"old": "sea", "new": "a b"}).Code; got != http.StatusBadRequest {
		t.Errorf("renaming to a bad tag: got %d, want 400", got)
	}
}

func TestDeleteEverywhere(t *testing.T) {
	a := newApp(t)
	a.upload("a.jpg", "hello", "")
	a.upload("b.jpg", "hello", "")
	tagChange(a, []string{"a.jpg", "b.jpg"}, []string{"beach", "keep"}, nil)
	if got := jsonOf(t, a.do("POST", "/api/tags/delete", map[string]string{"tag": "beach"})); got != `{"items":2}` {
		t.Errorf("deleting: got %s", got)
	}
	tagCheck(t, "counts", tagCounts(a), map[string]int{"keep": 2})
	if got := a.do("POST", "/api/tags/delete", map[string]string{"tag": "beach"}).Code; got != http.StatusNotFound {
		t.Errorf("deleting again: got %d, want 404", got)
	}
}

func TestTagsFollowRenamesAndMovesAllTheWayDown(t *testing.T) {
	a := newApp(t)
	a.upload("trip/day/a.jpg", "hello", "")
	a.do("POST", "/api/folders", map[string]string{"parent": "", "name": "archive"})
	tagChange(a, []string{"trip", "trip/day/a.jpg"}, []string{"x"}, nil)
	a.do("POST", "/api/rename", map[string]string{"path": "trip", "name": "holiday"})
	tagCheck(t, "renamed folder", a.listing("")["holiday"].Tags, []string{"x"})
	a.do("POST", "/api/move", map[string]any{"paths": []string{"holiday"}, "to": "archive"})
	tagCheck(t, "moved file", a.listing("archive/holiday/day")["a.jpg"].Tags, []string{"x"})
	tagCheck(t, "search", tagSearch(a, url.Values{"tag": {"x"}}), []string{"archive/holiday", "archive/holiday/day/a.jpg"})
}

func TestTagsWaitInTheTrashAndComeBackOnRestore(t *testing.T) {
	a := newApp(t)
	a.upload("trip/a.jpg", "hello", "")
	tagChange(a, []string{"trip", "trip/a.jpg"}, []string{"x"}, nil)
	names := tagTrash(a, "trip")
	tagCheck(t, "counts in the trash", tagCounts(a), map[string]int{})
	tagCheck(t, "search in the trash", tagSearch(a, url.Values{"tag": {"x"}}), []string{})
	a.do("POST", "/api/trash/restore", map[string]any{"names": names})
	tagCheck(t, "counts restored", tagCounts(a), map[string]int{"x": 2})
	tagCheck(t, "restored file", a.listing("trip")["a.jpg"].Tags, []string{"x"})
}

func TestRenamingOrDeletingATagReachesTheTrash(t *testing.T) {
	a := newApp(t)
	a.upload("a.jpg", "hello", "")
	a.upload("b.jpg", "hello", "")
	tagChange(a, []string{"a.jpg", "b.jpg"}, []string{"old", "gone"}, nil)
	names := tagTrash(a, "a.jpg")
	if got := jsonOf(t, a.do("POST", "/api/tags/rename", map[string]string{"old": "old", "new": "new"})); got != `{"items":1,"tag":"new"}` {
		t.Errorf("renaming: got %s", got)
	}
	if got := jsonOf(t, a.do("POST", "/api/tags/delete", map[string]string{"tag": "gone"})); got != `{"items":1}` {
		t.Errorf("deleting: got %s", got)
	}
	a.do("POST", "/api/trash/restore", map[string]any{"names": names})
	tagCheck(t, "restored file", a.listing("")["a.jpg"].Tags, []string{"new"})
}

func TestDeletingFromTheTrashDropsTheTags(t *testing.T) {
	for _, how := range []string{"delete", "empty"} {
		t.Run(how, func(t *testing.T) {
			a := newApp(t)
			a.upload("a.jpg", "hello", "")
			tagChange(a, []string{"a.jpg"}, []string{"x"}, nil)
			names := tagTrash(a, "a.jpg")
			var body any
			if how == "delete" {
				body = map[string]any{"names": names}
			}
			a.do("POST", "/api/trash/"+how, body)
			a.upload("a.jpg", "hello", "")
			tagCheck(t, "new file", a.listing("")["a.jpg"].Tags, []string{})
			if got := a.do("POST", "/api/tags/delete", map[string]string{"tag": "x"}).Code; got != http.StatusNotFound {
				t.Errorf("deleting the dropped tag: got %d, want 404", got)
			}
		})
	}
}

func TestTagsStayBehindWhenAFileMovesOutsideTheApp(t *testing.T) {
	a := newApp(t)
	a.upload("a.jpg", "hello", "")
	tagChange(a, []string{"a.jpg"}, []string{"x"}, nil)
	if err := os.Rename(filepath.Join(a.files, "a.jpg"), filepath.Join(a.files, "b.jpg")); err != nil {
		t.Fatal(err)
	}
	tagCheck(t, "moved away", a.listing("")["b.jpg"].Tags, []string{})
	tagCheck(t, "counts", tagCounts(a), map[string]int{})
	if err := os.Rename(filepath.Join(a.files, "b.jpg"), filepath.Join(a.files, "a.jpg")); err != nil {
		t.Fatal(err)
	}
	tagCheck(t, "moved back", a.listing("")["a.jpg"].Tags, []string{"x"})
}

func TestNewThingsDontPickUpTagsLeftBehind(t *testing.T) {
	a := newApp(t)
	removeTrip := func() {
		t.Helper()
		if err := os.Remove(filepath.Join(a.files, "trip", "a.jpg")); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(a.files, "trip")); err != nil {
			t.Fatal(err)
		}
		a.listing("")
	}
	a.upload("trip/a.jpg", "hello", "")
	tagChange(a, []string{"trip", "trip/a.jpg"}, []string{"x"}, nil)
	removeTrip()
	a.upload("trip/a.jpg", "hello", "") // makes the folder again
	tagCheck(t, "uploaded folder", a.listing("")["trip"].Tags, []string{})
	tagCheck(t, "uploaded file", a.listing("trip")["a.jpg"].Tags, []string{})
	tagChange(a, []string{"trip"}, []string{"y"}, nil)
	removeTrip()
	a.do("POST", "/api/folders", map[string]string{"parent": "", "name": "trip"})
	tagCheck(t, "new folder", a.listing("")["trip"].Tags, []string{})
}

func TestSearchByTags(t *testing.T) {
	a := newApp(t)
	a.upload("photos/beach.jpg", "hello", "")
	a.upload("photos/city.jpg", "hello", "")
	a.upload("docs/beach-trip.pdf", "hello", "")
	tagChange(a, []string{"photos", "photos/beach.jpg", "photos/city.jpg", "docs/beach-trip.pdf"}, []string{"2024"}, nil)
	tagChange(a, []string{"photos/beach.jpg", "docs/beach-trip.pdf"}, []string{"sea"}, nil)
	tagChange(a, []string{"docs/beach-trip.pdf"}, []string{"work"}, nil)
	for _, c := range []struct {
		query url.Values
		want  []string
	}{
		{url.Values{"tag": {"2024"}}, []string{"photos", "docs/beach-trip.pdf", "photos/beach.jpg", "photos/city.jpg"}},
		{url.Values{"tag": {"2024", "SEA"}}, []string{"docs/beach-trip.pdf", "photos/beach.jpg"}},
		{url.Values{"tag": {"sea"}, "not": {"work"}}, []string{"photos/beach.jpg"}},
		{url.Values{"not": {"2024"}}, []string{"docs"}},
		{url.Values{"tag": {"nope"}}, []string{}},
	} {
		tagCheck(t, "search "+c.query.Encode(), tagSearch(a, c.query), c.want)
	}
}

func TestSearchByNameIgnoresCaseAndSeparators(t *testing.T) {
	a := newApp(t)
	a.upload("Beach_House.jpg", "hello", "")
	a.upload("beach-house plans.pdf", "hello", "")
	a.upload("beach/other.txt", "hello", "")
	a.upload("beachhouse.txt", "hello", "")
	tagCheck(t, "beach  house", tagSearch(a, url.Values{"name": {"beach  house"}}), []string{"Beach_House.jpg", "beach-house plans.pdf"})
	tagCheck(t, "BEACH", tagSearch(a, url.Values{"name": {"BEACH"}}), []string{"beach", "Beach_House.jpg", "beach-house plans.pdf", "beachhouse.txt"})
	tagCheck(t, ".TXT", tagSearch(a, url.Values{"name": {".TXT"}}), []string{"beach/other.txt", "beachhouse.txt"})
	tagChange(a, []string{"beachhouse.txt"}, []string{"x"}, nil)
	tagCheck(t, "name and tag", tagSearch(a, url.Values{"name": {"beach"}, "tag": {"x"}}), []string{"beachhouse.txt"})
}

func TestSearchResultsCarryTagsAndFolderTotals(t *testing.T) {
	a := newApp(t)
	a.upload("trip/a.jpg", "123", "")
	tagChange(a, []string{"trip"}, []string{"x"}, nil)
	var body struct{ Items []db.Item }
	decodeInto(t, a.do("GET", "/api/search?tag=x", nil), &body)
	if len(body.Items) != 1 {
		t.Fatalf("found %d items, want 1: %+v", len(body.Items), body.Items)
	}
	item := body.Items[0]
	if item.Path != "trip" || !reflect.DeepEqual(item.Tags, []string{"x"}) || itemCount(item) != 1 || item.Size != 3 {
		t.Errorf("got path %q, tags %v, items %d, size %d; want trip, [x], 1, 3", item.Path, item.Tags, itemCount(item), item.Size)
	}
}

func TestSearchRefusesMoreTagsThanSqliteCanJoin(t *testing.T) {
	a := newApp(t)
	a.upload("a.txt", "hello", "")
	same := url.Values{}
	for range 50 {
		same.Add("tag", "t")
	}
	if got := a.do("GET", "/api/search?"+same.Encode(), nil).Code; got != http.StatusOK {
		t.Errorf("50 tags: got %d, want 200", got)
	}
	many := url.Values{}
	for i := range 51 {
		many.Add("tag", fmt.Sprintf("t%d", i))
	}
	response := a.do("GET", "/api/search?"+many.Encode(), nil)
	if got, want := fmt.Sprint(response.Code, " ", jsonOf(t, response)), `400 {"error":"Search for at most 50 tags at a time"}`; got != want {
		t.Errorf("51 tags: got %s, want %s", got, want)
	}
}

// The limit itself is tested in package db (TestSearchStopsAtItsLimit), since searchLimit is a const.
func TestSearchStopsAtItsLimit(t *testing.T) {
	a := newApp(t)
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		a.upload(name, "hello", "")
	}
	response := a.do("GET", "/api/search?name=txt", nil)
	var body struct {
		Items     []db.Item
		Truncated bool
	}
	decodeInto(t, response, &body)
	if len(body.Items) != 3 || body.Truncated {
		t.Errorf("under the limit: got %d items, truncated %v; want 3, false", len(body.Items), body.Truncated)
	}
	if got := jsonOf(t, a.do("GET", "/api/search", nil)); got != `{"items":[],"truncated":false}` {
		t.Errorf("empty search: got %s", got)
	}
}

func TestMovingOrTrashingAFolderWithSomethingInsideItPickedToo(t *testing.T) {
	a := newApp(t)
	a.upload("trip/a.jpg", "hello", "")
	a.do("POST", "/api/folders", map[string]string{"parent": "", "name": "archive"})
	if got := jsonOf(t, a.do("POST", "/api/move", map[string]any{"paths": []string{"trip/a.jpg", "trip"}, "to": "archive"})); got != `{"moved":1,"renamed":0}` {
		t.Errorf("moving: got %s", got)
	}
	if _, err := os.Stat(filepath.Join(a.files, "archive", "trip", "a.jpg")); err != nil {
		t.Errorf("moved file: %v", err)
	}
	if got := len(tagTrash(a, "archive/trip", "archive/trip/a.jpg")); got != 1 {
		t.Errorf("trashed %d items, want 1", got)
	}
}

func TestAZipFromSeveralFoldersNumbersNamesThatClash(t *testing.T) {
	a := newApp(t)
	a.upload("one/a.txt", "1", "")
	a.upload("two/A.txt", "2", "")
	response := a.do("POST", "/zip", url.Values{"paths": {"one/a.txt", "two/A.txt"}})
	if got := response.Header().Get("Content-Disposition"); !strings.Contains(got, "filename=files.zip") {
		t.Errorf("Content-Disposition: got %q, want it to name files.zip", got)
	}
	archive, err := zip.NewReader(bytes.NewReader(response.Body.Bytes()), int64(response.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, file := range archive.File {
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, _ := io.ReadAll(reader)
		reader.Close()
		got[file.Name] = string(content)
	}
	tagCheck(t, "zip contents", got, map[string]string{"a.txt": "1", "A (1).txt": "2"})
}
