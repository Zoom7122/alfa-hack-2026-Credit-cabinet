package signature

import (
	"bytes"
	"regexp"
)

// xmlSignatureRe — элемент Signature (с префиксом пространства имён или без).
var xmlSignatureRe = regexp.MustCompile(`<([A-Za-z_][\w.-]*:)?Signature[\s>/]`)

// EmbeddedSignature — есть ли в документе встроенная подпись: PDF со словарём подписи
// (/ByteRange и /Contents) — ContainerPDF, XML с подписью XMLDSig — ContainerXML; "" — нет.
// Подпись PDF затем проверяется (если она одна и покрывает весь файл), подпись XML — только находится.
func EmbeddedSignature(data []byte) Container {
	head := data[:min(len(data), 1024)]
	switch {
	case bytes.Contains(head, []byte("%PDF-")):
		if bytes.Contains(data, []byte("/ByteRange")) && bytes.Contains(data, []byte("/Contents")) {
			return ContainerPDF
		}
	case bytes.HasPrefix(bytes.TrimLeft(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")), " \t\r\n"), []byte("<")):
		if bytes.Contains(data, []byte("http://www.w3.org/2000/09/xmldsig#")) && xmlSignatureRe.Match(data) {
			return ContainerXML
		}
	}
	return ""
}
