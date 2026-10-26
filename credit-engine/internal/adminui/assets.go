// Package adminui embeds the standalone administrative browser interface.
package adminui

import (
	"embed"
	"path"
)

//go:embed web/*
var files embed.FS

func Asset(name string) ([]byte, string, bool) {
	contentType := ""
	switch name {
	case "index.html":
		contentType = "text/html; charset=utf-8"
	case "app.js", "api.js", "dom.js", "numbers.js", "forms.js", "model-edit.js", "editor.js", "rules-editor.js", "workbench.js", "audit.js":
		contentType = "text/javascript; charset=utf-8"
	case "styles.css":
		contentType = "text/css; charset=utf-8"
	default:
		return nil, "", false
	}
	data, err := files.ReadFile(path.Join("web", name))
	return data, contentType, err == nil
}
