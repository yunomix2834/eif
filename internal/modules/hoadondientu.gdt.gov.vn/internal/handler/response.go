package handler

import (
	"errors"
	"log/slog"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/yunomix2834/eif/internal/core/apperr"
)

const sessionHeader = "X-Session-ID"

func writeError(
	c *gin.Context,
	err error,
) {
	res := apperr.BuildResponse(
		err,
		c.GetString("request_id"),
	)
	slog.Warn(
		"HDDT GDT request failed",
		"method", c.Request.Method,
		"path", c.Request.URL.Path,
		"status", res.StatusCode,
		"code", res.Code,
		"request_id", res.RequestID,
		"error_chain", buildErrorChain(err),
	)
	c.JSON(
		res.StatusCode,
		res,
	)
}

func buildErrorChain(err error) string {
	const maxLength = 2048

	parts := make(
		[]string,
		0,
		4,
	)
	for current := err; current != nil; current = errors.Unwrap(current) {
		parts = append(
			parts,
			current.Error(),
		)
	}

	result := strings.Join(parts, ": ")
	if len(result) > maxLength {
		return result[:maxLength] + "..."
	}

	return result
}

func readSessionID(c *gin.Context) string {
	return c.GetHeader(sessionHeader)
}
