package httptransport

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/gin-gonic/gin"
)

type documentAccessService struct{ ApplicationService }

func (documentAccessService) Get(context.Context, string) (domain.Application, error) {
	return domain.Application{ID: "app", Company: domain.Company{ID: "company"}, Representative: domain.Representative{UserID: "author", HasLogin: true}}, nil
}

func TestDocumentAccess(t *testing.T) {
	for _, tc := range []struct {
		name, method string
		user         *domain.User
		want         int
	}{
		{name: "anonymous", method: "POST", want: 401},
		{name: "author", method: "POST", user: &domain.User{ID: "author"}, want: 204},
		{name: "outsider", method: "GET", user: &domain.User{ID: "outsider"}, want: 403},
		{name: "colleague reads", method: "GET", user: &domain.User{ID: "colleague", Companies: []domain.CompanyAccess{{CompanyID: "company"}}}, want: 204},
		{name: "colleague cannot upload", method: "POST", user: &domain.User{ID: "colleague", Companies: []domain.CompanyAccess{{CompanyID: "company"}}}, want: 403},
		{name: "bank reads", method: "GET", user: &domain.User{ID: "staff", Role: domain.UserUnderwriter}, want: 204},
		{name: "bank cannot replace client files", method: "DELETE", user: &domain.User{ID: "staff", Role: domain.UserUnderwriter}, want: 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := gin.New()
			router.Use(func(c *gin.Context) {
				if tc.user != nil {
					c.Set(userKey, *tc.user)
				}
				c.Next()
			})
			router.Handle(tc.method, "/applications/:id/documents", DocumentAccess(documentAccessService{}), func(c *gin.Context) { c.Status(204) })
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(tc.method, "/applications/app/documents", nil))
			if response.Code != tc.want {
				t.Fatalf("want %d, got %d: %s", tc.want, response.Code, response.Body.String())
			}
		})
	}
}
