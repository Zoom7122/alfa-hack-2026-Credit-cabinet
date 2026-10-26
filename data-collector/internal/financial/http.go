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

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/signature"

	"github.com/gin-gonic/gin"
)

// Register adds the document routes to the existing internal API. Identity/access
// controls must be provided by the same authenticated gateway as other application APIs.
func Register(r *gin.Engine, repo Repository, files Files) {
	register(r.Group("/api/v1"), repo, files, true)
}

// RegisterForCabinet adds financial routes to the existing API group. The cabinet
// already provides the document content and signature report routes.
func RegisterForCabinet(api *gin.RouterGroup, repo Repository, files Files) {
	register(api, repo, files, false)
}

func register(r *gin.RouterGroup, repo Repository, files Files, documentReads bool) {
	base := "/applications/:id/documents"
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
	//	@Summary		Загрузить документ и до двух откреплённых подписей
	//	@Description	Один file, обязательный type. Изменяет вложения только автор заявки в DRAFT или WAITING_FOR_INFO. Для отдельной SIGNATURE нужен signed_document_id; signatures тогда не передаются. Каждый файл до 20 МиБ. Ошибки разбора multipart возвращают JSON только с code; ошибки репозитория — Problem.
	//	@Tags			documents
	//	@Accept			multipart/form-data
	//	@Produce		json
	//	@Security		bearer
	//	@Param			id						path		string	true	"id заявки"
	//	@Param			file					formData	file	true	"Оригинал или отдельная подпись"
	//	@Param			type					formData	string	true	"Тип документа из DocumentType"
	//	@Param			signatures				formData	[]file	false	"До двух подписей к file"	collectionFormat(multi)
	//	@Param			signed_document_id		formData	string	false	"Оригинал для отдельной SIGNATURE"
	//	@Param			info_request_id			formData	string	false	"Вопрос банка, к которому относится вложение"
	//	@Success		201						{object}	Document
	//	@Failure		400,413					{object}	documentErrorResponse
	//	@Failure		401,403,404,409,422,500	{object}	documentErrorResponse
	//	@Router			/applications/{id}/documents [post]
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
		if !domain.DocumentType(typ).Valid() {
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
			mime := http.DetectContentType(raw)
			if strings.EqualFold(filepath.Ext(name), ".xml") {
				mime = "application/xml"
			}
			if strings.HasSuffix(strings.ToLower(name), ".sig") || strings.HasSuffix(strings.ToLower(name), ".p7s") {
				mime = "application/pkcs7-signature"
			}
			state := "NOT_SIGNED"
			if mime == "application/pkcs7-signature" || signature.EmbeddedSignature(raw) != "" {
				state = "PENDING"
			}
			return Document{ID: newID("doc_"), ApplicationID: c.Param("id"), Type: kind, FileName: name, MIME: mime, Size: int64(len(raw)), SHA256: hash(raw), StorageKey: key, SignedDocumentID: parent, InfoRequestID: c.PostForm("info_request_id"), Signatures: SignatureSummary{State: state}}, nil
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
	//	@Summary		Удалить документ и связанные подписи
	//	@Description	Только автор: в DRAFT либо в WAITING_FOR_INFO для документа открытого вопроса банка. Вместе с оригиналом удаляются его подписи.
	//	@Tags			documents
	//	@Security		bearer
	//	@Param			id					path	string	true	"id заявки"
	//	@Param			documentId			path	string	true	"id документа"
	//	@Success		204					"Удалено, без тела"
	//	@Failure		401,403,404,409,500	{object}	documentErrorResponse
	//	@Router			/applications/{id}/documents/{documentId} [delete]
	r.DELETE(base+"/:documentId", func(c *gin.Context) {
		if err := repo.Delete(c.Request.Context(), c.Param("id"), c.Param("documentId")); err != nil {
			fail(c, err)
			return
		}
		c.Status(204)
	})
	//	@Summary		Документы заявки с состояниями обработки
	//	@Description	Автор, представитель компании или андеррайтер; для анонимной заявки допускается запрос без токена.
	//	@Tags			documents
	//	@Produce		json
	//	@Security		bearer
	//	@Param			id				path		string	true	"id заявки"
	//	@Success		200				{object}	documentListResponse
	//	@Failure		401,403,404,500	{object}	documentErrorResponse
	//	@Router			/applications/{id}/documents [get]
	r.GET(base, func(c *gin.Context) {
		items, err := repo.List(c.Request.Context(), c.Param("id"))
		if err != nil {
			fail(c, err)
			return
		}
		c.JSON(200, gin.H{"items": items})
	})
	//	@Summary	Карточка документа
	//	@Tags		documents
	//	@Produce	json
	//	@Security	bearer
	//	@Param		id				path		string	true	"id заявки"
	//	@Param		documentId		path		string	true	"id документа"
	//	@Success	200				{object}	Document
	//	@Failure	401,403,404,500	{object}	documentErrorResponse
	//	@Router		/applications/{id}/documents/{documentId} [get]
	r.GET(base+"/:documentId", func(c *gin.Context) {
		d, err := repo.Get(c.Request.Context(), c.Param("id"), c.Param("documentId"))
		if err != nil {
			fail(c, err)
			return
		}
		c.JSON(200, d)
	})
	if documentReads {
		r.GET(base+"/:documentId/content", func(c *gin.Context) {
			d, err := repo.Get(c.Request.Context(), c.Param("id"), c.Param("documentId"))
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
	}
	//	@Summary		Результат разбора и проверки финансового документа
	//	@Description	202 state=PENDING означает, что сохранённого результата ещё нет. 200 содержит поколения, извлечённые показатели, диагностику и результат проверки подписи.
	//	@Tags			documents
	//	@Produce		json
	//	@Security		bearer
	//	@Param			id				path		string	true	"id заявки"
	//	@Param			documentId		path		string	true	"id документа"
	//	@Success		200				{object}	Result
	//	@Success		202				{object}	processingPendingResponse
	//	@Failure		401,403,404,500	{object}	documentErrorResponse
	//	@Router			/applications/{id}/documents/{documentId}/financial-result [get]
	r.GET(base+"/:documentId/financial-result", func(c *gin.Context) {
		app, id := c.Param("id"), c.Param("documentId")
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
	if documentReads {
		r.GET(base+"/:documentId/signature-report", func(c *gin.Context) {
			app, id := c.Param("id"), c.Param("documentId")
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
	}
	//	@Summary		Поставить повторную обработку документа и проверки подписи
	//	@Description	Создаёт новое поколение обработки, сохраняет исходные байты. Доступ и допустимый статус проверяются как при изменении вложений.
	//	@Tags			documents
	//	@Produce		json
	//	@Security		bearer
	//	@Param			id						path		string	true	"id заявки"
	//	@Param			documentId				path		string	true	"id документа"
	//	@Success		202						{object}	Document
	//	@Failure		401,403,404,409,422,500	{object}	documentErrorResponse
	//	@Router			/applications/{id}/documents/{documentId}/signature-verification [post]
	r.POST(base+"/:documentId/signature-verification", func(c *gin.Context) {
		if err := repo.Retry(c.Request.Context(), c.Param("id"), c.Param("documentId")); err != nil {
			fail(c, err)
			return
		}
		d, err := repo.Get(c.Request.Context(), c.Param("id"), c.Param("documentId"))
		if err != nil {
			fail(c, err)
			return
		}
		c.JSON(202, d)
	})
}
