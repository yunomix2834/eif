package middleware

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
)

func NewLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		slog.Info(
			"http request",
			"method",
			c.Request.Method,
			"path",
			c.Request.URL.Path,
			"status",
			c.Writer.Status(),
			"duration",
			time.Since(start),
			"request_id",
			c.GetString("request_id"),
		)
	}
}
