package httptransport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/service"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/storage"
)

const (
	questionnairePart     = "questionnaire" // часть multipart с JSON анкеты
	filesPart             = "files"         // части multipart с файлами
	maxQuestionnaireBytes = 1 << 20
)

// QuestionnaireService — то, что хендлеру анкет нужно от бизнес-слоя.
type QuestionnaireService interface {
	StageFile(name string, r io.Reader) (storage.Staged, error)
	Discard(files []storage.Staged)
	Submit(ctx context.Context, applicationID string, in service.SubmitInput) (service.Submitted, error)
}

// QuestionnaireHandler принимает анкету по заявке вместе с файлами. Маршруты — в router.go.
type QuestionnaireHandler struct {
	svc     QuestionnaireService
	maxBody int64 // лимит всего тела запроса
	log     *slog.Logger
}

// NewQuestionnaireHandler: maxBody — лимит тела запроса в байтах (все файлы и анкета).
func NewQuestionnaireHandler(svc QuestionnaireService, maxBody int64, log *slog.Logger) *QuestionnaireHandler {
	return &QuestionnaireHandler{svc: svc, maxBody: maxBody, log: log}
}

// submit — POST /api/v1/applications/{id}/questionnaire.
//
//	@Summary		Отправить анкету: заявка выходит из DRAFT, начинается сбор данных из моков
//	@Description	Анкета переводит заявку DRAFT → SUBMITTED и ставит задачу на сбор. Worker опрашивает моки
//	@Description	(заявка ENRICHING, задача RUNNING) и сохраняет ответы (VERIFYING, задача COMPLETED). Следить —
//	@Description	GET /applications/{id} или GET /collections/{id} из заголовка Location. Анкета по заявке
//	@Description	отправляется один раз (повтор — 409); для нового сбора зарегистрируйте заявителя ещё раз — будет новый черновик.
//	@Description
//	@Description	**Из Swagger UI:** Try it out → id заявки из ответа POST /applicants → в questionnaire уже подставлена
//	@Description	анкета сценария A (заявка на ИНН 7799000012; анкеты других сценариев — ниже) → Execute.
//	@Description	Файлы — в files через Add item: по одному полю на файл.
//	@Description
//	@Description	**Анкета** (часть questionnaire, JSON; ИНН компании берётся из заявки): обязательны age, marital_status,
//	@Description	children_count; product_code, requested, purpose пишутся в заявку. Без остальных данных система
//	@Description	не опрашивается и попадает в skipped_sources: consents.bki_company и bki_person — БКИ, consents.tax —
//	@Description	налоговая отчётность, director — Госуслуги и проверки руководителя, other_bank_biks — мультибанкинг,
//	@Description	marketplace_tokens — маркетплейсы, ofd_token — ОФД, collateral.cadastral_numbers — ЕГРН.
//	@Description
//	@Description	**Файлы** (части files): до 10, считая подписи, каждый до 20 МБ: pdf, docx, xlsx, jpg, png, xml,
//	@Description	подписи sig и p7s. Файл приходит целым или парой «документ + открепленная подпись»: balance.pdf +
//	@Description	balance.pdf.sig (или balance.sig); подпись без пары — присоединённая (документ внутри). Тип файла —
//	@Description	в documents анкеты: `[{"file_name": "balance.pdf", "type": "FINANCIAL_STATEMENTS"}]`, без него — OTHER.
//	@Description	Без файлов анкету можно прислать и как application/json. Одной транзакцией пишутся документы, анкета,
//	@Description	задача на сбор и события в outbox (о задаче и о документах — для будущих worker'ов подписи и OCR).
//	@Description
//	@Description.markdown	questionnaire
//	@Tags					questionnaires
//	@Accept					multipart/form-data
//	@Produce				json
//	@Param					id				path		string	true	"id заявки (app_…)"
//	@Param					questionnaire	formData	string	true	"JSON анкеты. Подставлен сценарий A без файлов — для заявки на ИНН 7799000012; другие сценарии — в описании"	extensions(x-example={"age":42,"marital_status":"MARRIED","children_count":2,"product_code":"WORKING_CAPITAL","requested":{"amount":5000000,"term_months":24},"purpose":{"code":"WORKING_CAPITAL","description":"Закупка товара под сезон"},"director":{"inn":"779900010139","last_name":"Ковалёв","first_name":"Игорь","middle_name":"Сергеевич","birth_date":"1984-07-19"},"consents":{"bki_company":{"id":"CNS-2026-000781","signed_at":"2026-10-21T14:02:11Z","valid_until":"2027-04-21"},"bki_person":{"id":"CNS-2026-000782","signed_at":"2026-10-21T14:02:40Z","valid_until":"2027-04-21"},"tax":{"id":"TAX-CNS-2026-000781","signed_at":"2026-10-21T14:03:02Z","valid_until":"2027-04-21","scope":["DECLARATIONS","ENS_BALANCE"]}},"other_bank_biks":["044525225"],"ofd_token":"demo-ofd-token","poa_number":"0f5a2c1e-7b3d-4e8a-9c21-6d4b8e0a1f37","representative_inn":"779900010333"})
//	@Param					files			formData	[]file	false	"до 10 файлов, считая подписи; документ и его подпись — balance.pdf и balance.pdf.sig"							collectionFormat(multi)
//	@Success				202				{object}	submittedResponse
//	@Header					202				{string}	Location	"/api/v1/collections/{id} — состояние задачи на сбор"
//	@Failure				400				{object}	problem		"битый JSON или multipart, нет обязательного поля (age, marital_status, children_count) или неверный тип"
//	@Failure				404				{object}	problem		"заявки нет"
//	@Failure				409				{object}	problem		"анкету по заявке уже отправили"
//	@Failure				413				{object}	problem		"запрос больше лимита"
//	@Failure				415				{object}	problem		"не multipart/form-data и не application/json"
//	@Failure				422				{object}	problem		"нарушено правило анкеты или файлов: формат, размер, пары подписей, больше 10 файлов"
//	@Failure				500				{object}	problem
//	@Security				bearer
//	@Router					/applications/{id}/questionnaire [post]
func (h *QuestionnaireHandler) submit(c *gin.Context) {
	var (
		req   questionnaireRequest
		files []storage.Staged
	)
	switch c.ContentType() {
	case binding.MIMEJSON:
		if err := c.ShouldBindJSON(&req); err != nil {
			writeProblem(c, bindingProblem(err))
			return
		}
	case binding.MIMEMultipartPOSTForm:
		var p *problem
		if files, p = h.readMultipart(c, &req); p != nil {
			h.svc.Discard(files)
			writeProblem(c, *p)
			return
		}
	default:
		writeProblem(c, unsupportedMediaProblem(
			"анкета — multipart/form-data (часть questionnaire и файлы files) или application/json"))
		return
	}

	in := req.toSubmitInput(files)
	if user, ok := currentUser(c); ok {
		in.ActorID = user.ID
	}
	res, err := h.svc.Submit(c.Request.Context(), c.Param("id"), in)
	if err != nil {
		var verr *service.ValidationError
		switch {
		case errors.Is(err, service.ErrForbidden):
			writeProblem(c, forbiddenProblem("анкету отправляет тот, кто оформил заявку"))
		case errors.Is(err, service.ErrUnauthorized):
			writeProblem(c, unauthorizedProblem("заявка оформлена тестовым пользователем — нужен его токен: Authorization: Bearer mock.<user_id>"))
		case errors.As(err, &verr):
			writeProblem(c, fromValidationError(verr, "анкета не прошла проверку"))
		case errors.Is(err, service.ErrNotFound):
			writeProblem(c, notFoundProblem("заявка не найдена"))
		case errors.Is(err, service.ErrInvalidTransition):
			writeProblem(c, conflictProblem("анкета по этой заявке уже отправлена"))
		default:
			h.log.ErrorContext(c.Request.Context(), "submit questionnaire", "err", err)
			writeProblem(c, internalProblem())
		}
		return
	}

	c.Header("Location", collectionsPath+"/"+res.Task.ID)
	c.JSON(http.StatusAccepted, toSubmittedResponse(res))
}

// readMultipart читает JSON анкеты и потоком принимает файлы во временную папку хранилища.
// При ошибке возвращает уже принятые файлы: их надо удалить.
func (h *QuestionnaireHandler) readMultipart(c *gin.Context, req *questionnaireRequest) ([]storage.Staged, *problem) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, h.maxBody)
	mr, err := c.Request.MultipartReader()
	if err != nil {
		p := validationProblem(http.StatusBadRequest, "некорректный multipart/form-data", nil)
		return nil, &p
	}

	var (
		files []storage.Staged
		data  []byte
	)
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			p := h.readProblem(err)
			return files, &p
		}
		switch part.FormName() {
		case questionnairePart:
			data, err = io.ReadAll(io.LimitReader(part, maxQuestionnaireBytes+1))
			if err != nil {
				p := h.readProblem(err)
				return files, &p
			}
			if len(data) > maxQuestionnaireBytes {
				p := tooLargeProblem("анкета больше 1 МБ")
				return files, &p
			}
		case filesPart:
			if len(files) == service.MaxFiles {
				p := validationProblem(http.StatusUnprocessableEntity, "анкета не прошла проверку", []problemField{{
					Field: "files", Code: "too_many", Message: fmt.Sprintf("не больше %d файлов, включая подписи", service.MaxFiles)}})
				return files, &p
			}
			st, err := h.svc.StageFile(part.FileName(), part)
			if err != nil {
				p := h.stageProblem(c, len(files), err)
				return files, &p
			}
			files = append(files, st)
		}
		_ = part.Close()
	}

	if data == nil {
		p := validationProblem(http.StatusBadRequest, "нет части questionnaire с JSON анкеты", []problemField{{
			Field: questionnairePart, Code: "required", Message: "обязательное поле"}})
		return files, &p
	}
	if err := binding.JSON.BindBody(data, req); err != nil {
		p := bindingProblem(err)
		return files, &p
	}
	return files, nil
}

// readProblem — ответ на ошибку разбора multipart: лимит тела → 413, иначе 400.
func (h *QuestionnaireHandler) readProblem(err error) problem {
	var mbe *http.MaxBytesError
	if errors.As(err, &mbe) {
		return tooLargeProblem(fmt.Sprintf("запрос больше %d МБ", h.maxBody>>20))
	}
	return validationProblem(http.StatusBadRequest, "некорректный multipart/form-data", nil)
}

// stageProblem — ответ на ошибку приёма i-го файла: больше лимита файла → 422, тела → 413,
// оборванная загрузка → 400, сбой записи на диск → 500.
func (h *QuestionnaireHandler) stageProblem(c *gin.Context, i int, err error) problem {
	var mbe *http.MaxBytesError
	switch {
	case errors.Is(err, service.ErrFileTooLarge):
		return validationProblem(http.StatusUnprocessableEntity, "анкета не прошла проверку", []problemField{{
			Field: fmt.Sprintf("files[%d]", i), Code: "too_large", Message: "файл больше лимита"}})
	case errors.As(err, &mbe):
		return tooLargeProblem(fmt.Sprintf("запрос больше %d МБ", h.maxBody>>20))
	case errors.Is(err, io.ErrUnexpectedEOF):
		return validationProblem(http.StatusBadRequest, "файл оборвался посреди загрузки", nil)
	default:
		h.log.ErrorContext(c.Request.Context(), "stage questionnaire file", "err", err)
		return internalProblem()
	}
}
