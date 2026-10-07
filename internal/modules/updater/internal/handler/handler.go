package handler

import (
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yunotools/eif/internal/core/apperr"
	"github.com/yunotools/eif/internal/modules/updater/internal/service"
)

type Handler struct {
	service       *service.Service
	onUpdateReady func()
	notifyOnce    sync.Once
}

func New(updateService *service.Service, onUpdateReady func()) *Handler {
	return &Handler{
		service:       updateService,
		onUpdateReady: onUpdateReady,
	}
}

func (h *Handler) CheckForUpdate(c *gin.Context) {
	info, err := h.service.CheckForUpdate(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, info)
}

func (h *Handler) InstallLatestUpdate(c *gin.Context) {
	info, err := h.service.InstallLatestUpdate(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusAccepted, info)
	c.Writer.Flush()
	if h.onUpdateReady == nil {
		return
	}
	h.notifyOnce.Do(func() {
		// Cho response HTTP đủ thời gian rời khỏi socket trước khi server shutdown.
		time.AfterFunc(250*time.Millisecond, h.onUpdateReady)
	})
}

func (h *Handler) writeError(c *gin.Context, err error) {
	errorCode := apperr.CodeUpdateFailed
	switch {
	case errors.Is(err, service.ErrNoUpdate):
		errorCode = apperr.CodeUpdateUnavailable
	case errors.Is(err, service.ErrInstallInProgress):
		errorCode = apperr.CodeUpdateInProgress
	}

	response := apperr.BuildResponse(
		apperr.New(errorCode, err),
		c.GetString("request_id"),
	)
	c.JSON(response.StatusCode, response)
}
