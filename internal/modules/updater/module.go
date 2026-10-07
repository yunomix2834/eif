package updater

import (
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/yunotools/eif/internal/core/config"
	"github.com/yunotools/eif/internal/modules/updater/internal/handler"
	"github.com/yunotools/eif/internal/modules/updater/internal/installer"
	"github.com/yunotools/eif/internal/modules/updater/internal/service"
	"github.com/yunotools/eif/internal/modules/updater/internal/source"
)

var ErrUnsupportedPlatform = installer.ErrUnsupportedPlatform

type Module struct {
	handler *handler.Handler
}

func New(
	cfg config.UpdaterConfig,
	currentVersion string,
	onUpdateReady func(),
) (*Module, error) {
	if onUpdateReady == nil {
		return nil, errors.New("update-ready callback is required")
	}

	platformInstaller, err := installer.New()
	if err != nil {
		return nil, err
	}
	releaseSource := source.NewGitHubSource(cfg.Repository, cfg.Timeout)
	updateService := service.New(releaseSource, platformInstaller, currentVersion)

	return &Module{
		handler: handler.New(updateService, onUpdateReady),
	}, nil
}

func (m *Module) RegisterRoutes(api *gin.RouterGroup) {
	group := api.Group("/updates")
	group.GET("/latest", m.handler.CheckForUpdate)
	group.POST("/install", m.handler.InstallLatestUpdate)
}
