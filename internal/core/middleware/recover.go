package middleware

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yunotools/eif/internal/core/apperr"
)

func NewRecovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				slog.Error(
					"panic recovered",
					"error",
					recovered,
					"request_id",
					c.GetString("request_id"),
				)

				res := apperr.BuildResponse(
					apperr.New(
						apperr.CodeInternalError,
						nil,
					),
					c.GetString("request_id"),
				)

				c.AbortWithStatusJSON(
					http.StatusInternalServerError,
					res,
				)
			}
		}()
		c.Next()
	}
}
