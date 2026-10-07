package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

const RequestIDHeader = "X-Request-ID"

func NewRequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetHeader(RequestIDHeader)
		if requestID == "" {
			requestID = newRequestID()
		}

		c.Set(
			"request_id",
			requestID,
		)
		c.Header(
			RequestIDHeader,
			requestID,
		)
		c.Next()

	}
}

func newRequestID() string {
	buf := make(
		[]byte,
		16,
	)
	if _, err := rand.Read(buf); err == nil {
		return hex.EncodeToString(buf)
	}

	return strconv.FormatInt(
		time.Now().UTC().UnixNano(),
		36,
	)
}
