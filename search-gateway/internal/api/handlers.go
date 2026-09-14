package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	_ "github.com/coolapso/searchbase/search-gateway/docs"
	"github.com/coolapso/searchbase/search-gateway/internal/scraper"
	"github.com/coolapso/searchbase/search-gateway/internal/search"
	"github.com/gin-gonic/gin"
)

// FetchRequest is the public REST payload for extracting one known URL.
type FetchRequest struct {
	URL      string `json:"url" binding:"required" example:"https://example.com" description:"The exact URL of the webpage to fetch"`
	JSRender bool   `json:"js_render" example:"false" description:"Whether to use a headless browser to execute JavaScript"`
}

// FetchResponse is the public REST response returned after URL extraction.
type FetchResponse struct {
	Title    string `json:"title"`
	URL      string `json:"url"`
	Snippet  string `json:"snippet"`
	Markdown string `json:"markdown,omitempty"`
}

// ErrorResponse is the common JSON error shape returned by REST handlers.
type ErrorResponse struct {
	Error string `json:"error"`
}

// APIServer owns REST handlers and their shared dependencies.
type APIServer struct {
	SearchProvider search.SearchProvider
	ScraperClient  *scraper.ScraperClient
	logger         *slog.Logger
}

// NewAPIServer wires REST handlers to the configured search provider and worker client.
func NewAPIServer(provider search.SearchProvider, scraperClient *scraper.ScraperClient, logger *slog.Logger) *APIServer {
	return &APIServer{
		SearchProvider: provider,
		ScraperClient:  scraperClient,
		logger:         logger.With(slog.String("component", "api")),
	}
}

// RegisterRoutes mounts the versioned REST API routes.
func (s *APIServer) RegisterRoutes(r *gin.RouterGroup) {
	r.POST("/search", s.HandleSearch)
	r.POST("/fetch", s.HandleFetch)
	r.GET("/healthz", s.HandleHealth)
}

// RegisterSwaggerRoutes exposes the generated Swagger UI.
func (s *APIServer) RegisterSwaggerRoutes(r *gin.Engine) {
	r.GET("/docs/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	r.GET("/docs", func(c *gin.Context) {
		c.Redirect(http.StatusMovedPermanently, "/docs/index.html")
	})
}

// @Summary  API Health Check
// @ID healthCheck
// @Description  Checks the API server is running and responds with a 200 status.
// @Tags  Health Check
// @Accept plain
// @Produce plain
// @Success 200 {string} OK
// @Failure 400 {object} ErrorResponse
// @Router /api/v1/healthz [get]
func (s *APIServer) HandleHealth(c *gin.Context) {
	c.String(http.StatusOK, "OK")
}

// @Summary Search the Web
// @ID webSearch
// @Description Use this tool to search the internet for a general query. Do NOT use this if you already have a specific URL.
// @Tags Search
// @Accept json
// @Produce json
// @Param request body search.Request true "Search request payload"
// @Success 200 {object} search.Results
// @Failure 400 {object} ErrorResponse
// @Failure 502 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/search [post]
func (s *APIServer) HandleSearch(c *gin.Context) {
	var req search.Request
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "invalid request format or missing required fields"})
		return
	}

	if req.Query == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "missing required field: query"})
		return
	}

	req.Normalize()

	results, err := s.SearchProvider.Search(c.Request.Context(), req)
	if err != nil {
		s.logger.Error("search provider error", "error", err)
		c.JSON(http.StatusBadGateway, ErrorResponse{Error: err.Error()})
		return
	}

	// Respond
	c.JSON(http.StatusOK, results)
}

// @Summary Get a website content
// @ID fetchUrl
// @Description Use this tool ONLY when you already have a specific, exact URL (http://...) that you want to extract and read. Do NOT pass search queries into this tool.
// @Tags fetchSingleUrl
// @Accept json
// @Produce json
// @Param request body FetchRequest true "Fetch request payload"
// @Success 200 {object} FetchResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/fetch [post]
func (s *APIServer) HandleFetch(c *gin.Context) {
	var req FetchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "invalid request format or missing required fields"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	markdown, scrapeErr := s.ScraperClient.Extract(ctx, req.URL, req.JSRender)
	if scrapeErr != nil {
		s.logger.Error("failed to scrape URL")
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to scrape URL"})
		return
	}

	c.JSON(http.StatusOK, FetchResponse{
		URL:      req.URL,
		Markdown: markdown,
	})
}
