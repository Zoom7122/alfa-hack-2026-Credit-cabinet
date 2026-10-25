package sigtest

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// PDFOptions — как собрать PDF с подписью внутри.
type PDFOptions struct {
	SubFilter    string      // "" — ETSI.CAdES.detached
	SigningTime  time.Time   // /M в словаре подписи; нулевое — без /M
	Sign         SignOptions // как собрать CMS; по умолчанию — без signing-time, как требует PAdES
	ContentsSize int         // сколько байт зарезервировать под подпись в /Contents; 0 — 8192
	NoSignature  bool        // оставить /Contents пустым (нули): поле подписи не подписано
}

// SignPDF собирает одностраничный PDF с текстом text (ASCII) и полем подписи и подписывает его,
// как это делают подписчики PAdES: /Contents резервируется нулями, /ByteRange покрывает весь
// файл, кроме значения /Contents, подпись CMS (открепленная, над этими байтами) записывается в hex.
func SignPDF(text string, s *Signer, opts PDFOptions) ([]byte, error) {
	if opts.SubFilter == "" {
		opts.SubFilter = "ETSI.CAdES.detached"
	}
	if opts.ContentsSize == 0 {
		opts.ContentsSize = 8192
	}
	const brPlaceholder = "[0 0000000000 0000000000 0000000000]"
	signedAt := ""
	if !opts.SigningTime.IsZero() {
		signedAt = " /M (D:" + opts.SigningTime.UTC().Format("20060102150405") + "+00'00')"
	}
	content := "BT /F1 14 Tf 72 760 Td (" + strings.NewReplacer(`\`, `\\`, "(", `\(`, ")", `\)`).Replace(text) + ") Tj ET"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [5 0 R] /SigFlags 3 >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Contents 4 0 R /Resources << /Font << /F1 7 0 R >> >> /Annots [5 0 R] >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Annot /Subtype /Widget /FT /Sig /T (Signature1) /Rect [0 0 0 0] /F 132 /P 3 0 R /V 6 0 R >>",
		"<< /Type /Sig /Filter /Adobe.PPKLite /SubFilter /" + opts.SubFilter + signedAt + " /ByteRange " + brPlaceholder +
			" /Contents <" + strings.Repeat("0", 2*opts.ContentsSize) + "> >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n")
	offsets := make([]int, len(objects))
	for i, obj := range objects {
		offsets[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, off := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	data := buf.Bytes()

	// /ByteRange: [0, начало /Contents) и (конец /Contents, конец файла]
	b := bytes.Index(data, []byte("/Contents <")) + len("/Contents ")
	c := b + 2*opts.ContentsSize + 2
	d := len(data) - c
	br := fmt.Sprintf("[0 %d %d %d]", b, c, d)
	br += strings.Repeat(" ", len(brPlaceholder)-len(br))
	at := bytes.Index(data, []byte(brPlaceholder))
	copy(data[at:], br)
	if opts.NoSignature {
		return data, nil
	}

	signed := append(append([]byte{}, data[:b]...), data[c:]...)
	cms, err := Sign(signed, s, opts.Sign)
	if err != nil {
		return nil, err
	}
	if len(cms) > opts.ContentsSize {
		return nil, fmt.Errorf("sigtest: подпись %d байт не помещается в /Contents (%d)", len(cms), opts.ContentsSize)
	}
	copy(data[b+1:], hex.EncodeToString(cms))
	return data, nil
}
