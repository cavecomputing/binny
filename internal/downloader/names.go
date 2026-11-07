package downloader

import (
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/cavecomputing/binny/internal/storage"
)

// ParseURL checks the link the client sent: http or https, with a host and no user name or
// password. Spaces, a "%" that starts no escape, and other characters a browser would escape are
// escaped.
func ParseURL(link string) (string, error) {
	var escaped strings.Builder
	link = strings.TrimSpace(link)
	for i := 0; i < len(link); i++ {
		b := link[i]
		lonePercent := b == '%' && (i+2 >= len(link) || !isHex(link[i+1]) || !isHex(link[i+2]))
		if b <= ' ' || b >= 0x7f || lonePercent {
			fmt.Fprintf(&escaped, "%%%02X", b) // %-escapes already there stay as they are
		} else {
			escaped.WriteByte(b)
		}
	}
	u, err := url.Parse(escaped.String())
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || !validPort(u.Port()) {
		return "", errors.New("Paste a link that starts with http:// or https://")
	}
	if u.User != nil {
		return "", errors.New("Links with a user name or password in them aren't supported")
	}
	u.Fragment, u.RawFragment = "", ""
	return u.String(), nil
}

func isHex(c byte) bool { return strings.IndexByte("0123456789abcdefABCDEF", c) >= 0 }

func validPort(port string) bool {
	if port == "" {
		return true
	}
	n := 0
	for _, digit := range port {
		n = n*10 + int(digit-'0')
		if n > 65535 {
			return false
		}
	}
	return n > 0
}

// safeName makes a name from the remote end one storage.CheckName accepts: no folders, control
// characters or leading dots, and short enough to take a " (1)". "download" when nothing usable
// is left.
func safeName(name string) string {
	name = storage.NameOf(strings.ReplaceAll(name, `\`, "/"))
	name = strings.Map(func(r rune) rune {
		if r < ' ' {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(name), "."))
	ext := path.Ext(name)
	name = storage.Shortened(name[:len(name)-len(ext)], 240-len(ext)) + ext
	if name, err := storage.CheckName(name); err == nil {
		return name
	}
	return "download"
}

func nameInURL(u *url.URL) string { return safeName(u.Path) }

// looseFilename finds the filename in a Content-Disposition too sloppy for mime to parse, such as
// an unquoted name with a space in it.
var looseFilename = regexp.MustCompile(`(?i)(?:^|;)\s*filename\s*=\s*(?:"([^"]*)"|([^;]*))`)

// nameFrom is the file's name: the server's Content-Disposition, else the end of the address it
// ended at, with an extension from the Content-Type when that has none.
func nameFrom(response *http.Response) string {
	name := nameInURL(response.Request.URL)
	disposition := response.Header.Get("Content-Disposition")
	given := ""
	if _, params, err := mime.ParseMediaType(disposition); err == nil {
		given = params["filename"]
	} else if match := looseFilename.FindStringSubmatch(disposition); match != nil {
		given = strings.TrimSpace(match[1] + match[2])
	}
	if given != "" {
		name = safeName(given)
	}
	contentType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if path.Ext(name) == "" && err == nil && contentType != "application/octet-stream" {
		name += storage.ExtensionFor(contentType)
	}
	return name
}
