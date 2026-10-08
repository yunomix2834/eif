package hddtgdt

import (
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/yunomix2834/eif/internal/core/config"
	corehttp "github.com/yunomix2834/eif/internal/core/protocol/httpclient"
	"github.com/yunomix2834/eif/internal/modules/hoadondientu.gdt.gov.vn/internal/client"
	"github.com/yunomix2834/eif/internal/modules/hoadondientu.gdt.gov.vn/internal/handler"
	"github.com/yunomix2834/eif/internal/modules/hoadondientu.gdt.gov.vn/internal/service"
	"github.com/yunomix2834/eif/internal/modules/hoadondientu.gdt.gov.vn/internal/session"
)

type Module struct {
	handler *handler.Handler
}

func New(
	httpClient *corehttp.Client,
	cfg config.HDDTGDTConfig,
) (
	*Module,
	error,
) {
	upstreamClient := client.New(
		httpClient,
		cfg.Endpoint,
	)
	sessionManager, err := session.NewManager(
		cfg.SessionSkew,
		cfg.SessionStorePath,
		cfg.SessionEncryptionKey,
	)
	if err != nil {
		return nil, err
	}

	slog.Info(
		"HDDT GDT session store loaded",
		"sessions",
		sessionManager.Count(),
		"store_path",
		cfg.SessionStorePath,
	)

	svc := service.New(
		upstreamClient,
		sessionManager,
		cfg.MaxQueryDays,
		cfg.MaxExportDays,
		cfg.MinRequestInterval,
		cfg.RateLimitRetries,
		cfg.RateLimitBaseDelay,
		cfg.QueryCacheTTL,
	)
	return &Module{handler: handler.New(svc)}, nil
}

func (m *Module) RegisterRoutes(api *gin.RouterGroup) {
	group := api.Group("/module/hoadondientu.gdt.gov.vn")
	group.GET(
		"/captcha",
		m.handler.GetCaptcha,
	)
	group.POST(
		"/authenticate",
		m.handler.Authenticate,
	)
	group.GET(
		"/session",
		m.handler.GetSession,
	)
	group.POST(
		"/session/refresh",
		m.handler.RefreshSession,
	)
	group.DELETE(
		"/session",
		m.handler.DeleteSession,
	)

	group.POST(
		"/invoice/sold",
		m.handler.QueryInvoiceSold,
	)
	group.POST(
		"/invoice/purchase",
		m.handler.QueryInvoicePurchase,
	)
	group.POST(
		"/invoice/sold/export",
		m.handler.ExportInvoiceSold,
	)
	group.POST(
		"/invoice/purchase/export",
		m.handler.ExportInvoicePurchase,
	)
}
