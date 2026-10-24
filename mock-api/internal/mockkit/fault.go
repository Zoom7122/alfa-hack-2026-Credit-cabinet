package mockkit

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// Middleware эхом возвращает X-Request-Id и по заголовку X-Mock-Fault
// имитирует отказ источника: timeout | 500 | 503 | captcha.
//
// timeout — сколько «висеть» на X-Mock-Fault: timeout (или до отмены запроса).
// captcha — ответ на X-Mock-Fault: captcha; nil — 403 CAPTCHA_REQUIRED.
func Middleware(timeout time.Duration, captcha gin.HandlerFunc) gin.HandlerFunc {
	if captcha == nil {
		captcha = func(c *gin.Context) {
			Error(c, http.StatusForbidden, CodeCaptchaRequired, "captcha required")
		}
	}
	return func(c *gin.Context) {
		if id := c.GetHeader("X-Request-Id"); id != "" {
			c.Header("X-Request-Id", id)
		}

		switch c.GetHeader("X-Mock-Fault") {
		case "":
			c.Next()
		case "500":
			Error(c, http.StatusInternalServerError, CodeInternalError, "mock fault: internal error")
		case "503":
			c.Header("Retry-After", "5")
			Error(c, http.StatusServiceUnavailable, CodeServiceUnavailable, "mock fault: service unavailable")
		case "timeout":
			t := time.NewTimer(timeout)
			defer t.Stop()
			select {
			case <-t.C:
			case <-c.Request.Context().Done():
			}
			Error(c, http.StatusGatewayTimeout, CodeTimeout, "mock fault: timeout")
		case "captcha":
			captcha(c)
			c.Abort()
		default:
			Error(c, http.StatusBadRequest, CodeBadRequest, "X-Mock-Fault must be one of: timeout, 500, 503, captcha")
		}
	}
}
