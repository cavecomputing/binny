package db

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cavecomputing/binny/internal/storage"
)

func TestCleanTagRefusesBadTags(t *testing.T) {
	for _, bad := range []string{"", "  ", "two words", "a,b", "old>new", "-x", "+x", "@x", strings.Repeat("x", 101), "tab\there", "nul\x00", "del\x7f"} {
		t.Run(bad, func(t *testing.T) {
			tag, err := CleanTag(bad)
			if err == nil || err.Error() == "" {
				t.Errorf("CleanTag(%q) = %q, %v; want an error with a message", bad, tag, err)
			}
		})
	}
}

func TestCleanTagTrimsAndLowercases(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"Family", "family"},
		{" beach ", "beach"},
		{"\tSEA\n", "sea"},
		{"x-ray", "x-ray"},
		{strings.Repeat("é", 100), strings.Repeat("é", 100)}, // the limit counts characters, not bytes
	} {
		if got, err := CleanTag(c.in); got != c.want || err != nil {
			t.Errorf("CleanTag(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
}

func TestWords(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"Beach_House.jpg", "beach house.jpg"},
		{"beach-house plans.pdf", "beach house plans.pdf"},
		{"beach  house", "beach house"},
		{"a_-_b", "a b"},
		{"  Beach__House  ", "beach house"}, // a search's name is trimmed too
		{"beachhouse.TXT", "beachhouse.txt"},
	} {
		if got := Words(c.in); got != c.want {
			t.Errorf("Words(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSearchStopsAtItsLimit(t *testing.T) {
	dataDir := t.TempDir()
	files := filepath.Join(dataDir, "files")
	if err := os.MkdirAll(files, 0o777); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		if err := os.WriteFile(filepath.Join(files, name), []byte("hello"), 0o666); err != nil {
			t.Fatal(err)
		}
	}
	disk, err := storage.OpenDisk(files)
	if err != nil {
		t.Fatal(err)
	}
	defer disk.Close()
	store, err := Open(filepath.Join(dataDir, "binny.db"), disk)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.IndexTree(""); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		limit     int
		want      []string
		truncated bool
	}{
		{2, []string{"a.txt", "b.txt"}, true},
		{3, []string{"a.txt", "b.txt", "c.txt"}, false},
	} {
		items, truncated, err := store.Search(nil, nil, "txt", c.limit)
		if err != nil {
			t.Fatal(err)
		}
		paths := []string{}
		for _, item := range items {
			paths = append(paths, item.Path)
		}
		if !reflect.DeepEqual(paths, c.want) || truncated != c.truncated {
			t.Errorf("limit %d: got %v, truncated %v; want %v, truncated %v", c.limit, paths, truncated, c.want, c.truncated)
		}
	}
}
