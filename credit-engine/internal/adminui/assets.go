// Package adminui embeds the standalone administrative browser interface.
package adminui

import (
	"embed"
	"path"
	"strings"
)

//go:embed dist
var files embed.FS

func Asset(name string) ([]byte, string, bool) {
	contentType := ""
	if path.Clean(name) != name || strings.HasPrefix(name, "/") {
		return nil, "", false
	}
	switch {
	case name == "index.html":
		contentType = "text/html; charset=utf-8"
	case strings.HasPrefix(name, "assets/") && path.Ext(name) == ".js":
		contentType = "text/javascript; charset=utf-8"
	case strings.HasPrefix(name, "assets/") && path.Ext(name) == ".css":
		contentType = "text/css; charset=utf-8"
	case strings.HasPrefix(name, "assets/") && path.Ext(name) == ".woff2":
		contentType = "font/woff2"
	default:
		return nil, "", false
	}
	data, err := files.ReadFile(path.Join("dist", name))
	return data, contentType, err == nil
}
