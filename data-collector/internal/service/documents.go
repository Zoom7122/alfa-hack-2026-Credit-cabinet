package service

import (
	"fmt"
	"path"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/storage"
)

// MaxFiles — сколько файлов принимает анкета, считая открепленные подписи.
const MaxFiles = 10

const maxFileNameBytes = 255

// fileKind — что за файл по расширению.
type fileKind struct {
	mime         string
	signature    bool // контейнер подписи (CMS): открепленная или присоединённая
	recognizable bool // есть что распознавать OCR
}

// fileKinds — допустимые расширения. MIME — свой список: в distroless нет /etc/mime.types.
var fileKinds = map[string]fileKind{
	".pdf":  {mime: "application/pdf", recognizable: true},
	".docx": {mime: "application/vnd.openxmlformats-officedocument.wordprocessingml.document", recognizable: true},
	".xlsx": {mime: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", recognizable: true},
	".jpg":  {mime: "image/jpeg", recognizable: true},
	".jpeg": {mime: "image/jpeg", recognizable: true},
	".png":  {mime: "image/png", recognizable: true},
	".xml":  {mime: "application/xml"},
	".sig":  {mime: "application/pkcs7-signature", signature: true},
	".p7s":  {mime: "application/pkcs7-signature", signature: true},
}

const allowedFormats = "pdf, docx, xlsx, jpg, png, xml; подписи — sig, p7s"

// DocumentSpec — тип документа, который клиент указал для файла.
type DocumentSpec struct {
	FileName string
	Type     domain.DocumentType
}

// plannedFile — принятый файл и документ, которым он станет.
type plannedFile struct {
	staged storage.Staged
	doc    domain.Document
}

// uploadEntry — файл запроса после проверки имени.
type uploadEntry struct {
	staged storage.Staged
	name   string // имя без пути
	lower  string
	ext    string
	kind   fileKind
	signs  int // для открепленной подписи — индекс подписанного документа, иначе -1
}

// planDocuments проверяет файлы анкеты и раскладывает их на документы и открепленные подписи.
// Подпись X.sig (X.p7s) относится к документу X; если такого нет, а у X нет расширения, —
// к единственному документу с той же основой имени (balance.sig → balance.pdf). Подпись без
// пары — присоединённая: документ внутри контейнера. Документы идут раньше своих подписей.
func planDocuments(applicationID, uploadedBy string, files []storage.Staged, specs []DocumentSpec,
	newID func() string, now time.Time) ([]plannedFile, []FieldError) {
	var fields []FieldError
	add := func(field, code, msg string) {
		fields = append(fields, FieldError{Field: field, Code: code, Message: msg})
	}
	fileField := func(i int) string { return fmt.Sprintf("files[%d]", i) }

	if len(files) > MaxFiles {
		add("files", "too_many", fmt.Sprintf("не больше %d файлов, включая подписи", MaxFiles))
		return nil, fields
	}

	entries := make([]uploadEntry, len(files))
	byName := make(map[string]int, len(files))
	for i, f := range files {
		name, ok := cleanFileName(f.Name)
		if !ok {
			add(fileField(i), "invalid_name", "имя файла пустое, длиннее 255 байт или с управляющими символами")
			continue
		}
		ext := strings.ToLower(path.Ext(name))
		kind, ok := fileKinds[ext]
		if !ok {
			add(fileField(i), "invalid_type", "допустимые форматы: "+allowedFormats)
			continue
		}
		if f.Size == 0 {
			add(fileField(i), "empty", "пустой файл")
		}
		lower := strings.ToLower(name)
		if j, dup := byName[lower]; dup {
			add(fileField(i), "duplicate", fmt.Sprintf("файл с таким именем уже есть: %s", fileField(j)))
			continue
		}
		byName[lower] = i
		entries[i] = uploadEntry{staged: f, name: name, lower: lower, ext: ext, kind: kind, signs: -1}
	}
	if len(fields) > 0 {
		return nil, fields
	}

	signedBy := map[int]int{} // документ → его открепленная подпись
	for i := range entries {
		e := &entries[i]
		if !e.kind.signature {
			continue
		}
		target := strings.TrimSuffix(e.lower, e.ext) // balance.pdf.sig → balance.pdf, balance.sig → balance
		doc := -1
		if j, ok := byName[target]; ok {
			if entries[j].kind.signature {
				add(fileField(i), "signature_of_signature", "подпись не может подписывать другую подпись")
				continue
			}
			doc = j
		} else if path.Ext(target) == "" {
			var matches []int
			for j, d := range entries {
				if !d.kind.signature && strings.TrimSuffix(d.lower, d.ext) == target {
					matches = append(matches, j)
				}
			}
			if len(matches) > 1 {
				add(fileField(i), "ambiguous_signature", fmt.Sprintf(
					"непонятно, какой документ подписан; назовите подпись по имени файла, например %s.sig", entries[matches[0]].name))
				continue
			}
			if len(matches) == 1 {
				doc = matches[0]
			}
		}
		if doc < 0 {
			continue // присоединённая подпись: документ внутри контейнера
		}
		if prev, ok := signedBy[doc]; ok {
			add(fileField(i), "second_signature", fmt.Sprintf("у %s уже есть подпись %s", entries[doc].name, fileField(prev)))
			continue
		}
		signedBy[doc] = i
		e.signs = doc
	}

	types := map[int]domain.DocumentType{}
	for k, sp := range specs {
		field := fmt.Sprintf("documents[%d]", k)
		j, ok := byName[strings.ToLower(strings.TrimSpace(sp.FileName))]
		switch {
		case !ok:
			add(field+".file_name", "unknown_file", "такого файла нет среди files")
		case entries[j].signs >= 0:
			add(field+".file_name", "signature_file", "это открепленная подпись: её тип всегда SIGNATURE")
		case !sp.Type.Valid() || sp.Type == domain.DocSignature:
			add(field+".type", "invalid_value", "тип документа из списка DocumentType, кроме SIGNATURE")
		default:
			if _, dup := types[j]; dup {
				add(field+".file_name", "duplicate", "тип для этого файла уже указан")
				continue
			}
			types[j] = sp.Type
		}
	}
	if len(fields) > 0 {
		return nil, fields
	}

	ids := make([]string, len(entries))
	for i := range entries {
		ids[i] = newID()
	}
	planned := make([]plannedFile, 0, len(entries))
	for _, signatures := range []bool{false, true} {
		for i, e := range entries {
			if (e.signs >= 0) != signatures {
				continue
			}
			doc := domain.Document{
				ID: ids[i], ApplicationID: applicationID, Type: domain.DocOther, FileName: e.name, MimeType: e.kind.mime,
				SizeBytes: e.staged.Size, SHA256: e.staged.SHA256, StorageKey: applicationID + "/" + ids[i] + e.ext,
				UploadedBy: uploadedBy, SignatureState: domain.SignatureNotSigned,
				RecognitionState: domain.RecognitionNotApplicable, UploadedAt: now,
			}
			if t, ok := types[i]; ok {
				doc.Type = t
			}
			if e.kind.recognizable {
				doc.RecognitionState = domain.RecognitionPending
			}
			_, hasSignature := signedBy[i]
			switch {
			case e.signs >= 0: // открепленная подпись
				doc.Type, doc.SignedDocumentID, doc.SignatureState = domain.DocSignature, ids[e.signs], domain.SignaturePending
			case e.kind.signature, hasSignature: // присоединённая подпись или документ с открепленной
				doc.SignatureState = domain.SignaturePending
			}
			planned = append(planned, plannedFile{staged: e.staged, doc: doc})
		}
	}
	// The supplied demo PDF and any attached signatures stay out of processing.
	testIDs := make(map[string]bool)
	for _, p := range planned {
		if p.doc.IsTest() {
			testIDs[p.doc.ID] = true
		}
	}
	for i := range planned {
		d := &planned[i].doc
		if testIDs[d.ID] || testIDs[d.SignedDocumentID] {
			d.SignatureState = domain.SignatureNotSigned
			d.RecognitionState = domain.RecognitionNotApplicable
		}
	}
	return planned, nil
}

// cleanFileName — имя файла без пути; false — пустое, слишком длинное или с управляющими символами.
func cleanFileName(name string) (string, bool) {
	name = path.Base(strings.ReplaceAll(strings.TrimSpace(name), `\`, "/"))
	if name == "." || name == "/" || name == ".." || len(name) > maxFileNameBytes || !utf8.ValidString(name) {
		return "", false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", false
		}
	}
	return name, true
}
