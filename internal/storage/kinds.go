package storage

import (
	"mime"
	"path"
	"strings"
)

// Kinds the explorer gives their own icon, by extension. Anything else is a plain "file".
var kinds = map[string][]string{
	"image":   {".png", ".jpg", ".jpeg", ".gif", ".webp", ".avif", ".bmp", ".svg", ".heic", ".tif", ".tiff", ".ico"},
	"video":   {".mp4", ".mkv", ".webm", ".mov", ".avi", ".m4v", ".wmv", ".flv", ".mpg", ".mpeg"},
	"audio":   {".mp3", ".flac", ".wav", ".ogg", ".opus", ".m4a", ".aac", ".wma"},
	"archive": {".zip", ".tar", ".gz", ".tgz", ".bz2", ".xz", ".zst", ".7z", ".rar", ".iso", ".img", ".dmg", ".deb", ".rpm"},
	"code":    {".py", ".js", ".html", ".css", ".json", ".yaml", ".yml", ".toml", ".sh", ".c", ".h", ".cpp", ".rs", ".go", ".java", ".rb", ".php", ".sql", ".xml"},
	"doc":     {".pdf", ".txt", ".md", ".doc", ".docx", ".odt", ".rtf", ".xls", ".xlsx", ".ods", ".csv", ".ppt", ".pptx", ".odp", ".epub", ".log"},
}

var kindByExtension = func() map[string]string {
	byExtension := map[string]string{}
	for kind, extensions := range kinds {
		for _, ext := range extensions {
			byExtension[ext] = kind
		}
	}
	return byExtension
}()

// Content types by extension, so they don't depend on the system's mime.types; the first
// extension of a type is the one a download without one is given.
var contentTypes = [][2]string{
	{".jpg", "image/jpeg"}, {".jpeg", "image/jpeg"}, {".png", "image/png"}, {".gif", "image/gif"},
	{".webp", "image/webp"}, {".avif", "image/avif"}, {".bmp", "image/bmp"}, {".svg", "image/svg+xml"},
	{".ico", "image/vnd.microsoft.icon"}, {".tif", "image/tiff"}, {".tiff", "image/tiff"},
	{".heic", "image/heic"}, {".heif", "image/heif"},
	{".mp4", "video/mp4"}, {".m4v", "video/x-m4v"}, {".mkv", "video/x-matroska"}, {".webm", "video/webm"},
	{".mov", "video/quicktime"}, {".avi", "video/x-msvideo"}, {".wmv", "video/x-ms-wmv"},
	{".flv", "video/x-flv"}, {".mpg", "video/mpeg"}, {".mpeg", "video/mpeg"},
	{".mp3", "audio/mpeg"}, {".flac", "audio/flac"}, {".wav", "audio/x-wav"}, {".ogg", "audio/ogg"},
	{".opus", "audio/opus"}, {".m4a", "audio/mp4"}, {".aac", "audio/aac"}, {".wma", "audio/x-ms-wma"},
	{".pdf", "application/pdf"},
	{".txt", "text/plain"}, {".log", "text/plain"}, {".srt", "text/plain"}, {".md", "text/markdown"},
	{".markdown", "text/markdown"}, {".csv", "text/csv"}, {".tsv", "text/tab-separated-values"},
	{".vtt", "text/vtt"}, {".rtf", "text/rtf"}, {".html", "text/html"}, {".htm", "text/html"},
	{".css", "text/css"}, {".js", "text/javascript"}, {".xml", "text/xml"},
	{".json", "application/json"}, {".zip", "application/zip"}, {".gz", "application/gzip"},
	{".tar", "application/x-tar"}, {".iso", "application/x-iso9660-image"},
}

// KindOf is what the explorer shows name as: "folder", one of kinds, or "file".
func KindOf(name string, isDir bool) string {
	if isDir {
		return "folder"
	}
	if kind, ok := kindByExtension[strings.ToLower(path.Ext(name))]; ok {
		return kind
	}
	return "file"
}

// ContentType is the type of a file called name, "application/octet-stream" when it's unknown.
func ContentType(name string) string {
	ext := strings.ToLower(path.Ext(name))
	for _, known := range contentTypes {
		if known[0] == ext {
			return known[1]
		}
	}
	if t, _, err := mime.ParseMediaType(mime.TypeByExtension(ext)); err == nil {
		return t
	}
	return "application/octet-stream"
}

// ExtensionFor is the extension a file of contentType gets, or "" for none known.
func ExtensionFor(contentType string) string {
	for _, known := range contentTypes {
		if known[1] == contentType {
			return known[0]
		}
	}
	if extensions, _ := mime.ExtensionsByType(contentType); len(extensions) > 0 {
		return extensions[0]
	}
	return ""
}
