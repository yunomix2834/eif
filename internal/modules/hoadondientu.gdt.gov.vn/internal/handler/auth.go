package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yunomix2834/eif/internal/core/apperr"
	"github.com/yunomix2834/eif/internal/modules/hoadondientu.gdt.gov.vn/internal/dto"
)

func (h *Handler) GetCaptcha(c *gin.Context) {
	result, err := h.service.GetCaptcha(c.Request.Context())
	if err != nil {
		writeError(
			c,
			err,
		)
		return
	}

	c.JSON(
		http.StatusOK,
		result,
	)
}

func (h *Handler) Authenticate(c *gin.Context) {
	var req dto.AuthenticationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(
			c,
			apperr.New(
				apperr.CodeInvalidRequest,
				err,
			),
		)
		return
	}

	result, err := h.service.Authenticate(
		c.Request.Context(),
		&req,
	)
	if err != nil {
		writeError(
			c,
			err,
		)
		return
	}

	c.JSON(
		http.StatusOK,
		result,
	)
}

func (h *Handler) GetSession(c *gin.Context) {
	result, err := h.service.GetSession(readSessionID(c))
	if err != nil {
		writeError(
			c,
			err,
		)
		return
	}

	c.JSON(
		http.StatusOK,
		result,
	)
}

func (h *Handler) RefreshSession(c *gin.Context) {
	var req dto.SessionRefreshRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(
			c,
			apperr.New(
				apperr.CodeInvalidRequest,
				err,
			),
		)
		return
	}

	result, err := h.service.RefreshSession(
		c.Request.Context(),
		readSessionID(c),
		&req,
	)
	if err != nil {
		writeError(
			c,
			err,
		)
		return
	}

	c.JSON(
		http.StatusOK,
		result,
	)
}

func (h *Handler) DeleteSession(c *gin.Context) {
	if err := h.service.DeleteSession(readSessionID(c)); err != nil {
		writeError(
			c,
			err,
		)
		return
	}

	c.Status(http.StatusNoContent)
}
