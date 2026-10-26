package httptransport

import (
	"errors"
	"net/http"
	"slices"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/service"
	"github.com/gin-gonic/gin"
)

// DocumentAccess uses the same applicant identity as answers; only the author may change attachments.
func DocumentAccess(apps ApplicationService) gin.HandlerFunc {
	return func(c *gin.Context) {
		app, err := apps.Get(c.Request.Context(), c.Param("id"))
		if err != nil {
			if errors.Is(err, service.ErrNotFound) {
				writeProblem(c, notFoundProblem("заявка не найдена"))
			} else {
				writeProblem(c, internalProblem())
			}
			return
		}
		user, loggedIn := currentUser(c)
		if !loggedIn && app.Representative.HasLogin {
			writeProblem(c, unauthorizedProblem("нужен вход в кабинет"))
			return
		}
		if loggedIn {
			allowed := user.ID == app.Representative.UserID
			if c.Request.Method == http.MethodGet {
				allowed = allowed || user.Role == domain.UserUnderwriter || slices.ContainsFunc(user.Companies, func(company domain.CompanyAccess) bool { return company.CompanyID == app.Company.ID })
			}
			if !allowed {
				writeProblem(c, forbiddenProblem("нет доступа к документам заявки"))
				return
			}
		}
		c.Next()
	}
}
