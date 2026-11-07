package storage

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
)

// storageDisk opens a Disk on a fresh files/ folder inside a temporary data folder.
func storageDisk(t *testing.T) (*Disk, string) {
	t.Helper()
	files := filepath.Join(t.TempDir(), "files")
	if err := os.Mkdir(files, 0o777); err != nil {
		t.Fatal(err)
	}
	d, err := OpenDisk(files)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d, files
}

func storageWrite(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("x"), 0o666); err != nil {
		t.Fatal(err)
	}
}

func wantInvalidPath(t *testing.T, err error, message string) {
	t.Helper()
	var invalid InvalidPath
	if !errors.As(err, &invalid) {
		t.Fatalf("got error %v, want InvalidPath", err)
	}
	if message != "" && string(invalid) != message {
		t.Errorf("got message %q, want %q", invalid, message)
	}
}

func TestCleanPathRefusesParentsHiddenNamesAndJunk(t *testing.T) {
	// Invalid UTF-8 is the junk a string can carry.
	for _, path := range []string{"..", "a/../..", ".trash", "a/.trash/b", ".binny-1.part", "a\x00b", "a\xffb"} {
		t.Run(path, func(t *testing.T) {
			got, err := CleanPath(path)
			if err == nil {
				t.Fatalf("CleanPath(%q) = %q, want InvalidPath", path, got)
			}
			wantInvalidPath(t, err, "Invalid path")
		})
	}
}

func TestCleanPathNormalizesSlashes(t *testing.T) {
	for path, want := range map[string]string{"/a//b/": "a/b", "": ""} {
		if got, err := CleanPath(path); err != nil || got != want {
			t.Errorf("CleanPath(%q) = %q, %v; want %q", path, got, err, want)
		}
	}
}

func TestResolveRefusesASymlinkThatLeadsOut(t *testing.T) {
	d, files := storageDisk(t)
	storageWrite(t, filepath.Join(filepath.Dir(files), "binny.db"))
	for name, target := range map[string]string{"out": filepath.Dir(files), "up": ".."} {
		if err := os.Symlink(target, filepath.Join(files, name)); err != nil {
			t.Fatal(err)
		}
		for _, path := range []string{name, name + "/binny.db"} {
			if f, err := d.Open(path); err == nil {
				f.Close()
				t.Errorf("Open(%q) through a link to %q succeeded, want an error", path, target)
			}
		}
		if _, err := d.ReadDir(name); err == nil {
			t.Errorf("ReadDir(%q) through a link to %q succeeded, want an error", name, target)
		}
		if _, err := d.Lstat(name + "/binny.db"); err == nil {
			t.Errorf("Lstat(%q/binny.db) succeeded, want an error", name)
		}
	}
}

func TestASymlinkThatStaysInsideIsFollowed(t *testing.T) {
	d, files := storageDisk(t)
	if err := os.Mkdir(filepath.Join(files, "real"), 0o777); err != nil {
		t.Fatal(err)
	}
	storageWrite(t, filepath.Join(files, "real", "a.txt"))
	if err := os.Symlink("real", filepath.Join(files, "alias")); err != nil {
		t.Fatal(err)
	}
	entries, err := d.ReadDir("alias")
	if err != nil || len(entries) != 1 || entries[0].Name() != "a.txt" {
		t.Fatalf("ReadDir(alias) = %v, %v; want [a.txt]", entries, err)
	}
	f, err := d.Open("alias/a.txt")
	if err != nil {
		t.Fatalf("Open(alias/a.txt): %v", err)
	}
	f.Close()
}

// os.Root refuses every absolute symlink, even one whose target is inside data/files/.
func TestAnAbsoluteSymlinkIsRefused(t *testing.T) {
	d, files := storageDisk(t)
	if err := os.Mkdir(filepath.Join(files, "real"), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(files, "real"), filepath.Join(files, "alias")); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ReadDir("alias"); err == nil {
		t.Fatal("ReadDir(alias) followed an absolute symlink")
	}
}

func TestCheckNameRefuses(t *testing.T) {
	for _, name := range []string{"", "   ", "a/b", ".hidden", "line\nbreak", strings.Repeat("x", 256)} {
		t.Run(name, func(t *testing.T) {
			got, err := CheckName(name)
			if err == nil {
				t.Fatalf("CheckName(%q) = %q, want InvalidPath", name, got)
			}
			wantInvalidPath(t, err, "")
		})
	}
}

func TestCheckNameStripsSurroundingSpaces(t *testing.T) {
	if got, err := CheckName("  notes.txt "); err != nil || got != "notes.txt" {
		t.Errorf(`CheckName("  notes.txt ") = %q, %v; want "notes.txt"`, got, err)
	}
}

func TestFreeNameNumbersATakenName(t *testing.T) {
	for _, tc := range []struct{ name, want string }{
		{"a.txt", "a (1).txt"},
		{"backup.tar.gz", "backup (1).tar.gz"},
		{"Backup.TAR.GZ", "Backup (1).TAR.GZ"},
		{"README", "README (1)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, files := storageDisk(t)
			storageWrite(t, filepath.Join(files, tc.name))
			if got := d.FreeName("", tc.name, false); got != tc.want {
				t.Errorf("FreeName(%q) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

func TestFreeNameCountsPastTakenNumbers(t *testing.T) {
	d, files := storageDisk(t)
	for _, name := range []string{"a.txt", "a (1).txt"} {
		storageWrite(t, filepath.Join(files, name))
	}
	if got := d.FreeName("", "a.txt", false); got != "a (2).txt" {
		t.Errorf(`FreeName("a.txt") = %q, want "a (2).txt"`, got)
	}
	if got := d.FreeName("", "free.txt", false); got != "free.txt" {
		t.Errorf(`FreeName("free.txt") = %q, want "free.txt"`, got)
	}
}

func TestFreeNameKeepsAFoldersDots(t *testing.T) {
	d, files := storageDisk(t)
	if err := os.MkdirAll(filepath.Join(files, "sub", "v1.2"), 0o777); err != nil {
		t.Fatal(err)
	}
	if got := d.FreeName("sub", "v1.2", true); got != "v1.2 (1)" {
		t.Errorf(`FreeName("sub", "v1.2") = %q, want "v1.2 (1)"`, got)
	}
}

func TestFreeNameShortensALongNameToFitItsNumber(t *testing.T) {
	d, files := storageDisk(t)
	name := strings.Repeat("é", 125) + ".txt" // 254 bytes
	storageWrite(t, filepath.Join(files, name))
	numbered := d.FreeName("", name, false)
	if want := strings.Repeat("é", 123) + " (1).txt"; numbered != want { // never half a character
		t.Errorf("FreeName = %q, want %q", numbered, want)
	}
	if len(numbered) > MaxNameBytes {
		t.Errorf("FreeName is %d bytes, want at most %d", len(numbered), MaxNameBytes)
	}
	storageWrite(t, filepath.Join(files, numbered)) // the disk takes it
}

func TestNumberedCountsOnFromTheName(t *testing.T) {
	var got []string
	for name := range Numbered("photo.jpg", false) {
		if got = append(got, name); len(got) == 3 {
			break
		}
	}
	if want := []string{"photo.jpg", "photo (1).jpg", "photo (2).jpg"}; !slices.Equal(got, want) {
		t.Errorf("Numbered = %q, want %q", got, want)
	}
}

func TestShortenedNeverCutsACharacter(t *testing.T) {
	for _, tc := range []struct {
		text     string
		maxBytes int
		want     string
	}{
		{"abc", 5, "abc"},
		{"abc", 2, "ab"},
		{"ééé", 5, "éé"},
		{"ééé", 1, ""},
		{"€uro", 2, ""},
		{"abc", -1, ""},
	} {
		if got := Shortened(tc.text, tc.maxBytes); got != tc.want {
			t.Errorf("Shortened(%q, %d) = %q, want %q", tc.text, tc.maxBytes, got, tc.want)
		}
	}
}

func TestPartialNameIsHiddenAndRecognized(t *testing.T) {
	name, other := PartialName(), PartialName()
	if name == other {
		t.Errorf("PartialName gave %q twice", name)
	}
	if !strings.HasPrefix(name, ".") || !IsPartial(name) {
		t.Errorf("PartialName() = %q, want a hidden name IsPartial accepts", name)
	}
	for _, name := range []string{".binny-1.part", "x" + name, name + ".txt", "binny-0123456789abcdef0123456789abcdef.part"} {
		if IsPartial(name) {
			t.Errorf("IsPartial(%q) = true, want false", name)
		}
	}
}

func TestKindComesFromTheExtension(t *testing.T) {
	for _, tc := range []struct {
		name  string
		isDir bool
		want  string
	}{
		{"Photo.JPG", false, "image"},
		{"photos.jpg", true, "folder"},
		{"mystery", false, "file"},
		{"clip.mkv", false, "video"},
		{"backup.tar.gz", false, "archive"},
	} {
		if got := KindOf(tc.name, tc.isDir); got != tc.want {
			t.Errorf("KindOf(%q, %v) = %q, want %q", tc.name, tc.isDir, got, tc.want)
		}
	}
}

func TestContentTypeAndExtensionFor(t *testing.T) {
	for name, want := range map[string]string{
		"Photo.JPG": "image/jpeg", "page.html": "text/html", "song.mp3": "audio/mpeg",
		"doc.pdf": "application/pdf", "mystery": "application/octet-stream", "x.nosuchext": "application/octet-stream",
	} {
		if got := ContentType(name); got != want {
			t.Errorf("ContentType(%q) = %q, want %q", name, got, want)
		}
	}
	for contentType, want := range map[string]string{
		"image/jpeg": ".jpg", "video/mp4": ".mp4", "text/plain": ".txt", "application/x-no-such-type": "",
	} {
		if got := ExtensionFor(contentType); got != want {
			t.Errorf("ExtensionFor(%q) = %q, want %q", contentType, got, want)
		}
	}
}

func TestPlainAndIsFolder(t *testing.T) {
	d, files := storageDisk(t)
	storageWrite(t, filepath.Join(files, "a.txt"))
	if err := os.Mkdir(filepath.Join(files, "folder"), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("folder", filepath.Join(files, "link")); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(files, "pipe"), 0o666); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path            string
		plain, isFolder bool
	}{
		{"a.txt", true, false},
		{"folder", true, true},
		{"", true, true},
		{"link", false, false},
		{"pipe", false, false},
		{"missing", false, false},
	} {
		if _, plain := d.Plain(tc.path); plain != tc.plain {
			t.Errorf("Plain(%q) = %v, want %v", tc.path, plain, tc.plain)
		}
		if got := d.IsFolder(tc.path); got != tc.isFolder {
			t.Errorf("IsFolder(%q) = %v, want %v", tc.path, got, tc.isFolder)
		}
	}
}

func TestMakeFolders(t *testing.T) {
	d, files := storageDisk(t)
	created, err := d.MakeFolders("a/b/c")
	if want := []string{"a", "a/b", "a/b/c"}; err != nil || !slices.Equal(created, want) {
		t.Fatalf("MakeFolders(a/b/c) = %q, %v; want %q", created, err, want)
	}
	if created, err := d.MakeFolders("a/b/d"); err != nil || !slices.Equal(created, []string{"a/b/d"}) {
		t.Errorf("MakeFolders(a/b/d) = %q, %v; want [a/b/d]", created, err)
	}
	if created, err := d.MakeFolders(""); err != nil || len(created) != 0 {
		t.Errorf(`MakeFolders("") = %q, %v; want none`, created, err)
	}

	storageWrite(t, filepath.Join(files, "a", "f.txt"))
	if err := os.Symlink("b", filepath.Join(files, "a", "link")); err != nil {
		t.Fatal(err)
	}
	for path, part := range map[string]string{"a/f.txt/x": "f.txt", "a/link/x": "link"} {
		_, err := d.MakeFolders(path)
		wantInvalidPath(t, err, `"`+part+`" is a file, not a folder`)
	}
	if d.IsFolder("a/b/x") {
		t.Error("a/b/x was made through the link")
	}
}
