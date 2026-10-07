package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func NewCORS(
	allowedOrigins []string,
) gin.HandlerFunc {
	allowed := make(
		map[string]struct{},
		len(allowedOrigins),
	)
	for _, origin := range allowedOrigins {
		allowed[strings.TrimSpace(origin)] = struct{}{}
	}

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" {
			if _, ok := allowed[origin]; ok {
				c.Header(
					"Access-Control-Allow-Origin",
					origin,
				)
				c.Header(
					"Vary",
					"Origin",
				)
				c.Header(
					"Access-Control-Allow-Headers",
					"Accept, Content-Type, Authorization, X-Request-ID, X-Session-ID",
				)
				c.Header(
					"Access-Control-Allow-Methods",
					"GET, POST, DELETE, OPTIONS, PUT, PATCH",
				)

				// Content-Disposition không nằm trong nhóm response header mà
				// JavaScript được phép đọc mặc định khi request cross-origin.
				//
				// Frontend cần đọc header này để lấy đúng tên file backend trả về.
				c.Header(
					"Access-Control-Expose-Headers",
					"Content-Disposition, X-Request-ID",
				)
			}
		}

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}
