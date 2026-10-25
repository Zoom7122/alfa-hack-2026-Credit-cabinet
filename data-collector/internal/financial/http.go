package financial

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

// Register adds the document routes to the existing internal API. Identity/access
// controls must be provided by the same authenticated gateway as other application APIs.
func Register(r *gin.Engine, repo Repository, files Files) {
	base := "/api/v1/applications/:applicationId/documents"
	fail := func(c *gin.Context, err error) {
		status, code := 500, "INTERNAL"
		switch {
		case errors.Is(err, ErrNotFound):
			status, code = 404, "NOT_FOUND"
		case errors.Is(err, ErrUnsupported):
			status, code = 422, "UNSUPPORTED"
		case errors.Is(err, ErrProcessingFailed):
			status, code = 422, "PROCESSING_FAILED"
		case errors.Is(err, ErrConflict):
			status, code = 409, "CONFLICT"
		}
		c.Header("Content-Type", "application/problem+json")
		c.AbortWithStatusJSON(status, gin.H{"type": "about:blank", "title": code, "status": status, "code": code})
	}
	r.POST(base, func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 3*MaxFileSize+(1<<20))
		if err := c.Request.ParseMultipartForm(1 << 20); err != nil {
			var large *http.MaxBytesError
			if errors.As(err, &large) {
				c.JSON(413, gin.H{"code": "FILE_TOO_LARGE"})
			} else {
				c.JSON(400, gin.H{"code": "INVALID_MULTIPART"})
			}
			return
		}
		defer c.Request.MultipartForm.RemoveAll()
		form := c.Request.MultipartForm
		if len(form.File["file"]) != 1 || len(form.File["signatures"]) > 2 {
			c.JSON(400, gin.H{"code": "INVALID_FILES"})
			return
		}
		typ := c.PostForm("type")
		target := c.PostForm("signed_document_id")
		if typ != "TAX_RETURN" && typ != "FINANCIAL_STATEMENTS" && typ != "SIGNATURE" {
			fail(c, ErrUnsupported)
			return
		}
		if (typ == "SIGNATURE" && (target == "" || len(form.File["signatures"]) > 0)) || (typ != "SIGNATURE" && target != "") {
			c.JSON(400, gin.H{"code": "INVALID_SIGNATURE_LINK"})
			return
		}
		stored := []string{}
		committed := false
		defer func() {
			if !committed {
				for _, key := range stored {
					path, _ := files.Path(key)
					os.Remove(path)
				}
			}
		}()
		save := func(fh *multipart.FileHeader, kind, parent string) (Document, error) {
			f, err := fh.Open()
			if err != nil {
				return Document{}, err
			}
			defer f.Close()
			raw, err := io.ReadAll(io.LimitReader(f, MaxFileSize+1))
			if err != nil {
				return Document{}, err
			}
			if len(raw) == 0 || len(raw) > MaxFileSize {
				return Document{}, ErrUnsupported
			}
			key, err := files.Put(raw)
			if err != nil {
				return Document{}, err
			}
			stored = append(stored, key)
			name := filepath.Base(strings.ReplaceAll(fh.Filename, "\\", "/"))
			return Document{ID: newID("doc_"), ApplicationID: c.Param("applicationId"), Type: kind, FileName: name, MIME: http.DetectContentType(raw), Size: int64(len(raw)), SHA256: hash(raw), StorageKey: key, SignedDocumentID: parent}, nil
		}
		d, err := save(form.File["file"][0], typ, target)
		if err != nil {
			fail(c, err)
			return
		}
		sigs := []Document{}
		for _, fh := range form.File["signatures"] {
			s, e := save(fh, "SIGNATURE", d.ID)
			if e != nil {
				fail(c, e)
				return
			}
			sigs = append(sigs, s)
		}
		if err = repo.Upload(c.Request.Context(), d, sigs); err != nil {
			fail(c, err)
			return
		}
		committed = true
		d, err = repo.Get(c.Request.Context(), d.ApplicationID, d.ID)
		if err != nil {
			fail(c, err)
			return
		}
		c.JSON(201, d)
	})
	r.GET(base, func(c *gin.Context) {
		items, err := repo.List(c.Request.Context(), c.Param("applicationId"))
		if err != nil {
			fail(c, err)
			return
		}
		c.JSON(200, gin.H{"items": items})
	})
	r.GET(base+"/:documentId", func(c *gin.Context) {
		d, err := repo.Get(c.Request.Context(), c.Param("applicationId"), c.Param("documentId"))
		if err != nil {
			fail(c, err)
			return
		}
		c.JSON(200, d)
	})
	r.GET(base+"/:documentId/content", func(c *gin.Context) {
		d, err := repo.Get(c.Request.Context(), c.Param("applicationId"), c.Param("documentId"))
		if err != nil {
			fail(c, err)
			return
		}
		b, err := files.Read(d.StorageKey)
		if err != nil {
			fail(c, err)
			return
		}
		if hash(b) != d.SHA256 {
			fail(c, errors.New("stored original corrupted"))
			return
		}
		c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": d.FileName}))
		c.Header("X-Content-Type-Options", "nosniff")
		c.Data(200, "application/octet-stream", b)
	})
	r.GET(base+"/:documentId/financial-result", func(c *gin.Context) {
		app, id := c.Param("applicationId"), c.Param("documentId")
		if _, err := repo.Get(c.Request.Context(), app, id); err != nil {
			fail(c, err)
			return
		}
		raw, err := repo.Result(c.Request.Context(), app, id)
		if errors.Is(err, ErrNotFound) {
			c.JSON(202, gin.H{"state": "PENDING"})
			return
		}
		if err != nil {
			fail(c, err)
			return
		}
		c.Data(200, "application/json", raw)
	})
	r.GET(base+"/:documentId/signature-report", func(c *gin.Context) {
		app, id := c.Param("applicationId"), c.Param("documentId")
		if _, err := repo.Get(c.Request.Context(), app, id); err != nil {
			fail(c, err)
			return
		}
		raw, err := repo.Result(c.Request.Context(), app, id)
		if errors.Is(err, ErrNotFound) {
			c.JSON(202, gin.H{"state": "PENDING"})
			return
		}
		if err != nil {
			fail(c, err)
			return
		}
		var result Result
		if err = json.Unmarshal(raw, &result); err != nil {
			fail(c, err)
			return
		}
		c.JSON(200, result.Signature)
	})
	r.POST(base+"/:documentId/signature-verification", func(c *gin.Context) {
		if err := repo.Retry(c.Request.Context(), c.Param("applicationId"), c.Param("documentId")); err != nil {
			fail(c, err)
			return
		}
		d, err := repo.Get(c.Request.Context(), c.Param("applicationId"), c.Param("documentId"))
		if err != nil {
			fail(c, err)
			return
		}
		c.JSON(202, d)
	})
}
