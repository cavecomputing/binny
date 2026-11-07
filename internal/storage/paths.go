// Package storage holds the rules for paths and names under data/files/, and Disk, the only way
// to reach the files there.
package storage

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"iter"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"
)

// MaxNameBytes is the most a file or folder name can take on disk (Linux and most filesystems).
const MaxNameBytes = 255

// InvalidPath is a path or name from the client that Binny refuses. Its text is shown to the user.
type InvalidPath string

func (e InvalidPath) Error() string { return string(e) }

// Kept whole when " (1)" is slotted in front of an extension.
var doubleExtensions = []string{".tar.gz", ".tar.bz2", ".tar.xz", ".tar.zst"}

// A file being uploaded or downloaded, hidden until it's complete (see PartialName).
var partialName = regexp.MustCompile(`^\.binny-[0-9a-f]{32}\.part$`)

// CleanPath normalizes a client path to "a/b" ("" for the top folder). It refuses "..", NUL and any
// hidden part: Binny keeps its own things (.trash, partial files) under dot-names.
func CleanPath(p string) (string, error) {
	if strings.ContainsRune(p, 0) || !utf8.ValidString(p) {
		return "", InvalidPath("Invalid path")
	}
	var parts []string
	for part := range strings.SplitSeq(p, "/") {
		if strings.HasPrefix(part, ".") {
			return "", InvalidPath("Invalid path")
		}
		if part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, "/"), nil
}

// CheckName checks a new file or folder name from the client, stripped of surrounding spaces.
func CheckName(name string) (string, error) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return "", InvalidPath("A name is required")
	case strings.ContainsFunc(name, func(r rune) bool { return r == '/' || r < 32 }) || !utf8.ValidString(name):
		return "", InvalidPath(`Names can't contain "/" or control characters`)
	case strings.HasPrefix(name, "."):
		return "", InvalidPath(`Names can't start with "." (Binny hides those)`)
	case len(name) > MaxNameBytes:
		return "", InvalidPath("That name is too long")
	}
	return name, nil
}

// Numbered yields name, then "name (1).ext", "name (2).ext" and so on, without end. A long name
// loses the end of its stem to make room for the number.
func Numbered(name string, isDir bool) iter.Seq[string] {
	stem, ext := name, ""
	if !isDir {
		ext = path.Ext(name)
		for _, double := range doubleExtensions {
			if strings.HasSuffix(strings.ToLower(name), double) {
				ext = name[len(name)-len(double):]
			}
		}
		stem = name[:len(name)-len(ext)]
	}
	return func(yield func(string) bool) {
		if !yield(name) {
			return
		}
		for n := 1; ; n++ {
			number := fmt.Sprintf(" (%d)%s", n, ext)
			if !yield(Shortened(stem, MaxNameBytes-len(number)) + number) {
				return
			}
		}
	}
}

// Shortened cuts text to at most maxBytes of UTF-8, never inside a character.
func Shortened(text string, maxBytes int) string {
	if len(text) <= maxBytes {
		return text
	}
	maxBytes = max(maxBytes, 0)
	for maxBytes > 0 && !utf8.RuneStart(text[maxBytes]) {
		maxBytes--
	}
	return text[:maxBytes]
}

// PartialName is a new hidden name to write a file under before renaming it into place, so a
// half-written file never shows under its real name. Scanning its folder deletes one left behind.
func PartialName() string {
	random := make([]byte, 16)
	rand.Read(random)
	return ".binny-" + hex.EncodeToString(random) + ".part"
}

// IsPartial reports whether name is one PartialName made.
func IsPartial(name string) bool { return partialName.MatchString(name) }

func Child(folder, name string) string {
	if folder == "" {
		return name
	}
	return folder + "/" + name
}

func ParentOf(rel string) string {
	i := strings.LastIndexByte(rel, '/')
	if i < 0 {
		return ""
	}
	return rel[:i]
}

func NameOf(rel string) string { return rel[strings.LastIndexByte(rel, '/')+1:] }
