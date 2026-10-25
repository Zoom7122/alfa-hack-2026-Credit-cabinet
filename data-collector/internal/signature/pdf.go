package signature

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Подпись внутри PDF (PAdES; ISO 32000-1, 12.8). Проверяется только строгий случай: в файле одна
// подпись, её /ByteRange начинается с нуля и доходит до последнего байта, а единственный
// непокрытый промежуток — значение /Contents этой подписи: hex-строка с CMS и выравнивающими
// нулями. Тогда подписаны все байты файла, кроме самой подписи, и незаметно изменить документ
// нельзя. Файл, дописанный после подписания (инкрементальные изменения), несколько подписей и
// устаревшие SubFilter — INDETERMINATE: какие изменения после подписи допустимы (DocMDP), не разбирается.

var (
	byteRangeRe = regexp.MustCompile(`/ByteRange\s*\[\s*(\d{1,12})\s+(\d{1,12})\s+(\d{1,12})\s+(\d{1,12})\s*\]`)
	subFilterRe = regexp.MustCompile(`/SubFilter\s*/([A-Za-z0-9._-]+)`)
	pdfTimeRe   = regexp.MustCompile(`/M\s*\((D:[0-9+\-Z']+)\)`)
	pdfObjRe    = regexp.MustCompile(`\d+\s+\d+\s+obj\b`)
)

// pdfSignature — подпись из PDF: что подписано и сама подпись.
type pdfSignature struct {
	byteRange [4]int
	subFilter string
	signed    []byte    // подписанные байты: data[a:a+b] и data[c:c+d]
	cms       []byte    // подпись CMS из /Contents без выравнивающих нулей
	claimed   time.Time // /M — время подписи, заявленное подписантом; нулевое — не указано
}

// parsePDFSignature находит подпись в PDF и проверяет её охват. Ошибка — *formatError с кодом
// проверки: unsupported — вывод сделать нельзя (INDETERMINATE), иначе подпись не покрывает
// документ как следует (INVALID).
func parsePDFSignature(data []byte) (*pdfSignature, error) {
	matches := byteRangeRe.FindAllSubmatchIndex(data, -1)
	switch {
	case len(matches) == 0:
		return nil, &formatError{code: "PDF_SIGNATURE", unsupported: true, msg: "словарь подписи PDF не разобран: нет /ByteRange из четырёх чисел"}
	case len(matches) > 1:
		return nil, &formatError{code: "SINGLE_SIGNER", unsupported: true,
			msg: fmt.Sprintf("в PDF %d подписи (или подпись и метка времени документа): MVP проверяет PDF только с одной подписью", len(matches))}
	}
	m := matches[0]
	var br [4]int
	for i := range br {
		n, err := strconv.Atoi(string(data[m[2+2*i]:m[3+2*i]]))
		if err != nil {
			return nil, &formatError{code: "SIGNED_DATA_COVERAGE", msg: "в /ByteRange не числа"}
		}
		br[i] = n
	}
	a, b, c, d := br[0], br[1], br[2], br[3]
	coverage := func(format string, args ...any) error {
		return &formatError{code: "SIGNED_DATA_COVERAGE", msg: fmt.Sprintf(format, args...)}
	}
	switch size := len(data); {
	case a != 0:
		return nil, coverage("подпись не покрывает начало файла: /ByteRange начинается с %d, а не с 0", a)
	case b < 1 || c < b+2 || c > size || d > size-c:
		return nil, coverage("/ByteRange [%d %d %d %d] не соответствует файлу размером %d байт", a, b, c, d, size)
	case c+d < size:
		return nil, &formatError{code: "DOCUMENT_NOT_MODIFIED", unsupported: true,
			msg: fmt.Sprintf("после подписания в файл дописано %d байт (инкрементальное изменение): допустимы ли такие изменения, "+
				"не проверяется", size-c-d)}
	}

	// единственный непокрытый промежуток — значение /Contents: <hex-строка>
	hole := data[b:c]
	if hole[0] != '<' || hole[len(hole)-1] != '>' || !bytes.HasSuffix(bytes.TrimRight(data[:b], " \t\r\n"), []byte("/Contents")) {
		return nil, coverage("непокрытый подписью промежуток — не значение /Contents подписи")
	}
	decoded, err := hex.DecodeString(strings.Map(func(r rune) rune {
		if r == ' ' || r == '\t' || r == '\r' || r == '\n' {
			return -1
		}
		return r
	}, string(hole[1:len(hole)-1])))
	if err != nil {
		return nil, coverage("в /Contents не только hex-строка подписи")
	}
	if allZero(decoded) {
		return nil, &formatError{code: "PDF_SIGNATURE", unsupported: true, msg: "поле подписи в PDF не заполнено: в /Contents одни нули"}
	}
	var scan berScan
	n, ok := scan.element(decoded, 0)
	if !ok {
		return nil, &formatError{code: "FORMAT_PARSED", msg: "подпись CMS в /Contents повреждена"}
	}
	if !allZero(decoded[n:]) {
		return nil, coverage("в /Contents после подписи посторонние данные: разрешены только выравнивающие нули")
	}

	// SubFilter и /M — в объекте, где лежит /ByteRange (эти байты тоже подписаны)
	objStart := 0
	if objs := pdfObjRe.FindAllIndex(data[:m[0]], -1); len(objs) > 0 {
		objStart = objs[len(objs)-1][0]
	}
	objEnd := len(data)
	if i := bytes.Index(data[m[1]:], []byte("endobj")); i >= 0 {
		objEnd = m[1] + i
	}
	dict := data[objStart:objEnd]
	ps := &pdfSignature{byteRange: br, cms: decoded[:n]}
	if sf := subFilterRe.FindSubmatch(dict); sf != nil {
		ps.subFilter = string(sf[1])
	}
	switch ps.subFilter {
	case "adbe.pkcs7.detached", "ETSI.CAdES.detached":
	case "adbe.pkcs7.sha1":
		return nil, &formatError{code: "PDF_SIGNATURE", unsupported: true,
			msg: "устаревший SubFilter adbe.pkcs7.sha1 (подпись над SHA-1 хешем документа) не поддерживается"}
	case "ETSI.RFC3161":
		return nil, &formatError{code: "PDF_SIGNATURE", unsupported: true, msg: "это метка времени документа (ETSI.RFC3161), а не подпись"}
	case "":
		return nil, &formatError{code: "PDF_SIGNATURE", unsupported: true, msg: "в словаре подписи PDF нет /SubFilter"}
	default:
		return nil, &formatError{code: "PDF_SIGNATURE", unsupported: true, msg: "SubFilter " + ps.subFilter + " не поддерживается"}
	}
	if t := pdfTimeRe.FindSubmatch(dict); t != nil {
		ps.claimed, _ = parsePDFDate(string(t[1]))
	}
	ps.signed = append(append(make([]byte, 0, b+d), data[:b]...), data[c:c+d]...)
	return ps, nil
}

// parsePDFDate разбирает дату PDF «D:ГГГГММДДЧЧммСС+ЧЧ'мм'» (ISO 32000-1, 7.9.4). Время без
// секунд и пояса допускается; без часов — нет: такой точности для проверки срока мало.
func parsePDFDate(s string) (time.Time, bool) {
	s = strings.TrimPrefix(s, "D:")
	i := 0
	for i < len(s) && i < 14 && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	digits, rest := s[:i], s[i:]
	if len(digits) < 10 || len(digits)%2 != 0 {
		return time.Time{}, false
	}
	digits += "000000"[:14-len(digits)]
	loc := time.UTC
	if rest != "" && rest[0] != 'Z' {
		tz := strings.ReplaceAll(rest[1:], "'", "")
		if (rest[0] != '+' && rest[0] != '-') || len(tz) < 2 {
			return time.Time{}, false
		}
		hh, err1 := strconv.Atoi(tz[:2])
		mm := 0
		var err2 error
		if len(tz) >= 4 {
			mm, err2 = strconv.Atoi(tz[2:4])
		}
		if err1 != nil || err2 != nil {
			return time.Time{}, false
		}
		offset := hh*3600 + mm*60
		if rest[0] == '-' {
			offset = -offset
		}
		loc = time.FixedZone("", offset)
	}
	t, err := time.ParseInLocation("20060102150405", digits, loc)
	if err != nil {
		return time.Time{}, false
	}
	return t.UTC(), true
}

func allZero(b []byte) bool {
	for _, x := range b {
		if x != 0 {
			return false
		}
	}
	return true
}
