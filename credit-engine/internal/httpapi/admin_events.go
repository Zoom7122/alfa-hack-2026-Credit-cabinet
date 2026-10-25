package httpapi

import (
	"context"
	"errors"
	"mime"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/adminauth"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/adminstore"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/events"
	"github.com/gin-gonic/gin"
)

type AdminEvents interface {
	SaveEvent(context.Context, contracts.AdminEvent) (adminstore.StoredEvent, error)
	GetEvent(context.Context, string) (adminstore.StoredEvent, error)
	ListEvents(context.Context, adminstore.EventFilter) ([]adminstore.EventSummary, error)
}

func writeEventError(c *gin.Context, err error) {
	var invalid *events.Error
	if errors.As(err, &invalid) {
		writeProblem(c, invalid.Status, invalid.Code, invalid.Fields)
		return
	}
	if errors.Is(err, adminstore.ErrConflict) {
		writeProblem(c, 409, "EVENT_CONFLICT", nil)
		return
	}
	writeAdminError(c, err)
}
func RegisterAdminEvents(router *gin.Engine, access *adminauth.Access, store AdminEvents) {
	router.POST("/api/v1/credit-events", access.Service(), func(c *gin.Context) {
		media, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
		if err != nil || media != "application/json" {
			writeProblem(c, 415, "JSON_REQUIRED", nil)
			return
		}
		event, err := events.Decode(c.Request.Body)
		if err != nil {
			writeEventError(c, err)
			return
		}
		stored, err := store.SaveEvent(c.Request.Context(), event)
		if err != nil {
			writeEventError(c, err)
			return
		}
		// Repository success means the transaction has committed. An uncertain
		// commit returns an error; an MS2 retry safely finds the persisted event.
		c.JSON(202, contracts.EventAck{EventID: stored.EventID, Accepted: true})
	})
	g := router.Group("/api/admin/v1", access.Browser())
	g.GET("/events", func(c *gin.Context) {
		filter, ok := eventFilter(c)
		if !ok {
			return
		}
		result, err := store.ListEvents(c.Request.Context(), filter)
		if err != nil {
			writeEventError(c, err)
			return
		}
		c.JSON(200, result)
	})
	// An application ID is an opaque string. A catch-all preserves escaped '/'
	// and '+' inside the derived event ID without a second URL-unescape pass.
	g.GET("/events/*event_id", func(c *gin.Context) {
		id := strings.TrimPrefix(c.Param("event_id"), "/")
		if strings.TrimSpace(id) == "" {
			writeProblem(c, 400, "INVALID_EVENT_ID", nil)
			return
		}
		result, err := store.GetEvent(c.Request.Context(), id)
		if err != nil {
			writeEventError(c, err)
			return
		}
		c.JSON(200, result)
	})
}
func eventFilter(c *gin.Context) (adminstore.EventFilter, bool) {
	filter := adminstore.EventFilter{Page: adminstore.Page{Limit: 50}}
	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		writeProblem(c, 400, "INVALID_EVENT_FILTER", nil)
		return filter, false
	}
	bad := func() (adminstore.EventFilter, bool) {
		writeProblem(c, 400, "INVALID_EVENT_FILTER", nil)
		return adminstore.EventFilter{}, false
	}
	for key, values := range query {
		if len(values) != 1 || values[0] == "" {
			return bad()
		}
		value := values[0]
		switch key {
		case "limit", "offset":
			number, err := strconv.Atoi(value)
			if err != nil || number < 0 || key == "limit" && (number < 1 || number > 200) {
				return bad()
			}
			if key == "limit" {
				filter.Page.Limit = number
			} else {
				filter.Page.Offset = number
			}
		case "kind":
			filter.Kind = contracts.EventType(value)
			if filter.Kind != contracts.EventTypeLoanIssued && filter.Kind != contracts.EventTypeBorrowerReview && filter.Kind != contracts.EventTypeBankReview {
				return bad()
			}
		case "borrower_type":
			filter.BorrowerType = contracts.BorrowerType(value)
			if filter.BorrowerType != contracts.BorrowerTypeSoleProprietor && filter.BorrowerType != contracts.BorrowerTypeLegalEntity {
				return bad()
			}
		case "application_id":
			filter.ApplicationID = value
		case "borrower_id":
			filter.BorrowerID = value
		case "model_version":
			filter.ModelVersion = value
		case "from", "to":
			stamp, err := time.Parse(time.RFC3339, value)
			if err != nil {
				return bad()
			}
			if key == "from" {
				filter.From = &stamp
			} else {
				filter.To = &stamp
			}
		default:
			return bad()
		}
	}
	if filter.From != nil && filter.To != nil && filter.From.After(*filter.To) {
		return bad()
	}
	return filter, true
}
