package router

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yunomix2834/eif/internal/core/buildinfo"
	"github.com/yunomix2834/eif/internal/core/config"
	"github.com/yunomix2834/eif/internal/core/middleware"
	coremodule "github.com/yunomix2834/eif/internal/core/module"
	appweb "github.com/yunomix2834/eif/web"
)

func BuildEngine(
	cfg *config.Config,
	registrars ...coremodule.Registrar,
) *gin.Engine {
	r := gin.New()

	r.Use(
		middleware.NewRequestID(),
		middleware.NewLogger(),
		middleware.NewRecovery(),
		middleware.NewCORS(cfg.CORS.AllowedOrigins),
	)

	r.GET(
		"/healthz",
		func(c *gin.Context) {
			c.JSON(
				http.StatusOK,
				gin.H{
					"status":              "ok",
					"version":             buildinfo.Version,
					"backend_commit":      buildinfo.BackendCommit,
					"frontend_commit":     buildinfo.FrontendCommit,
					"orchestrator_commit": buildinfo.OrchestratorCommit,
				},
			)
		},
	)

	api := r.Group("/api/v1")
	for _, registrar := range registrars {
		registrar.RegisterRoutes(api)
	}

	r.NoRoute(
		createStaticFallback(
			cfg.Server.StaticDir,
			appweb.GetStaticFS(),
		),
	)
	return r
}
