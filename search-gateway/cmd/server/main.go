package main

import (
	"context"
	"log"
	"net/http"

	"github.com/coolapso/go-utils/slogger"
	"github.com/coolapso/searchbase/search-gateway/internal/api"
	"github.com/coolapso/searchbase/search-gateway/internal/mcp"
	"github.com/coolapso/searchbase/search-gateway/internal/middlewares"
	"github.com/coolapso/searchbase/search-gateway/internal/scraper"
	"github.com/coolapso/searchbase/search-gateway/internal/search"
	"github.com/coolapso/searchbase/search-gateway/internal/settings"
	"github.com/coolapso/searchbase/search-gateway/internal/telemetry"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

// @title  Searchbase Search Gateway API
// @version  1.0
// @description  Bot Accessible Search Engine for web searching and URL fetching.
func main() {
	s, err := settings.NewSettings()
	if err != nil {
		log.Fatalf("initialization failed: %v\n", err)
	}

	logger, err := slogger.NewLogger("info", "json")
	if err != nil {
		log.Fatalf("failed to create logger: %v\n", err)
	}

	searchProvider, err := search.NewProvider(s.SearchProvider())
	if err != nil {
		log.Fatalf("failed to create search provider: %v\n", err)
	}

	scraperClient := scraper.NewScraperClient(s.CrawlWorkerAddress())

	gin.SetMode(s.GinMode())
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middlewares.GinSlogger(logger))

	if s.Otel().Tracing().Enabled() {
		tp, err := telemetry.InitTracer("search-gateway", s.Otel().Tracing().Endpoint())
		if err != nil {
			log.Fatalf("failed to initialize tracer: %v\n", err)
		}
		defer func() {
			if err := tp.Shutdown(context.Background()); err != nil {
				log.Fatalf("failed to shutdown tracer: %v\n", err)
			}
		}()
		r.Use(otelgin.Middleware("search-gateway"))
	}

	apiServer := api.NewAPIServer(searchProvider, scraperClient, logger)
	v1 := r.Group("/api/v1")
	apiServer.RegisterRoutes(v1)
	apiServer.RegisterSwaggerRoutes(r)

	mcpServer := mcp.NewServer(searchProvider, scraperClient, logger)
	mcpServer.RegisterRoutes(r, s.Address(), s.Mcp().HeartbeatEnabled(), s.Mcp().HeartbeatInterval())

	logger.Info("Starting searchbase Gateway",
		"port", s.Port(),
		"search_backend", s.SearchProvider().Name(),
		"search_provider_address", s.SearchProvider().Address(),
		"crawl_worker", s.CrawlWorkerAddress(),
		"mcp_heartbeat_enabled", s.Mcp().HeartbeatEnabled(),
		"mcp_heartbeat_interval", s.Mcp().HeartbeatInterval(),
	)

	if err := r.Run(":" + s.Port()); err != nil && err != http.ErrServerClosed {
		logger.Error("Server failed to start", "error", err)
	}
}
