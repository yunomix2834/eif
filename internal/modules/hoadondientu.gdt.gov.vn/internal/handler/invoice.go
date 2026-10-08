package handler

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yunomix2834/eif/internal/core/apperr"
	"github.com/yunomix2834/eif/internal/modules/hoadondientu.gdt.gov.vn/internal/dto"
	"github.com/yunomix2834/eif/internal/modules/hoadondientu.gdt.gov.vn/internal/model"
)

func (h *Handler) QueryInvoiceSold(c *gin.Context) {
	h.query(
		c,
		h.service.QueryInvoiceSold,
	)
}

func (h *Handler) QueryInvoicePurchase(c *gin.Context) {
	h.query(
		c,
		h.service.QueryInvoicePurchase,
	)
}

func (h *Handler) query(
	c *gin.Context,
	fn func(
		context.Context,
		string,
		*dto.HoaDonQuery,
	) (
		*model.InvoiceQueryResult,
		error,
	),
) {
	var req dto.HoaDonQuery
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

	result, err := fn(
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
