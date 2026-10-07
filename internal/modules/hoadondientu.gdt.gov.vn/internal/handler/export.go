package handler

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yunotools/eif/internal/core/apperr"
	"github.com/yunotools/eif/internal/modules/hoadondientu.gdt.gov.vn/internal/dto"
	"github.com/yunotools/eif/internal/modules/hoadondientu.gdt.gov.vn/internal/model"
)

func (h *Handler) ExportInvoiceSold(c *gin.Context) {
	h.export(
		c,
		h.service.ExportInvoiceSold,
	)
}

func (h *Handler) ExportInvoicePurchase(c *gin.Context) {
	h.export(
		c,
		h.service.ExportInvoicePurchase,
	)
}

func (h *Handler) export(
	c *gin.Context,
	fn func(
		context.Context,
		string,
		*dto.ExportInvoiceRequest,
	) (
		*model.File,
		error,
	),
) {
	var req dto.ExportInvoiceRequest
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

	file, err := fn(
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

	writeHeaderExportFile(
		c,
		file,
	)
}

func writeHeaderExportFile(
	c *gin.Context,
	file *model.File,
) {
	c.Header(
		"Content-Disposition",
		fmt.Sprintf(
			"attachment; filename=%q",
			file.Filename,
		),
	)
	c.Data(
		http.StatusOK,
		file.ContentType,
		file.Body,
	)
}
