package settings

import (
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

// Settings contains all gateway configuration after environment parsing and validation.
type Settings struct {
	port               string
	crawlWorkerAddress string
	searchProvider     *SearchProvider
	ginMode            string
	address            string
	logLevel           string
	otel               *Otel
	mcp                *Mcp
}

type providerSpec struct {
	addressEnv string
	tokenEnv   string
}

var providerSpecs = map[string]providerSpec{
	"searchbase_ddg": {},
	"ddgs":           {addressEnv: "DDGS_PROVIDER_ADDRESS"},
	"searxng":        {addressEnv: "SEARXNG_PROVIDER_ADDRESS"},
	"brave":          {tokenEnv: "BRAVE_API_TOKEN"},
	"mojeek":         {tokenEnv: "MOJEEK_API_KEY"},
}

func (s *Settings) Port() string                    { return s.port }
func (s *Settings) CrawlWorkerAddress() string      { return s.crawlWorkerAddress }
func (s *Settings) SearchProvider() *SearchProvider { return s.searchProvider }
func (s *Settings) GinMode() string                 { return s.ginMode }
func (s *Settings) Address() string                 { return s.address }
func (s *Settings) LogLevel() string                { return s.logLevel }
func (s *Settings) Otel() *Otel                     { return s.otel }
func (s *Settings) Mcp() *Mcp                       { return s.mcp }

// NewSettings loads SEARCHBASE_* environment variables, applies defaults, and
// validates provider, tracing and mcp configuration.
func NewSettings() (*Settings, error) {
	v := viper.New()
	v.SetEnvPrefix("SEARCHBASE")
	v.AutomaticEnv()

	v.SetDefault("PORT", "8080")
	v.SetDefault("CRAWL_WORKER_ADDRESS", "http://localhost:8000")
	v.SetDefault("SEARCH_PROVIDER", "searchbase_ddg")
	v.SetDefault("LOG_LEVEL", "error")
	v.SetDefault("ENVIRONMENT", "production")
	v.SetDefault("MCP_HEARTBEAT_ENABLED", false)
	v.SetDefault("MCP_HEARTBEAT_INTERVAL", 60)

	providerName := v.GetString("SEARCH_PROVIDER")
	if _, ok := providerSpecs[providerName]; !ok {
		return nil, fmt.Errorf("unsupported search provider: %s", providerName)
	}

	var providerAddress string
	var token string
	if providerSpecs[providerName].addressEnv != "" {
		providerAddress = v.GetString(providerSpecs[providerName].addressEnv)
		if providerAddress == "" {
			return nil, fmt.Errorf("provider address not set: searchbase provider %s requires SEARCHBASE_%s", providerName, providerSpecs[providerName].addressEnv)
		}
	}

	if providerSpecs[providerName].tokenEnv != "" {
		token = v.GetString(providerSpecs[providerName].tokenEnv)
		if token == "" {
			return nil, fmt.Errorf("provider API Token not set: searchbase provider %s requires SEARCHBASE_%s", providerName, providerSpecs[providerName].tokenEnv)
		}
	}

	s := &Settings{
		port:               v.GetString("PORT"),
		crawlWorkerAddress: v.GetString("CRAWL_WORKER_ADDRESS"),
		address:            v.GetString("ADDRESS"),
		logLevel:           strings.ToLower(v.GetString("LOG_LEVEL")),
		ginMode:            gin.ReleaseMode,
		searchProvider: &SearchProvider{
			name:    providerName,
			address: providerAddress,
			token:   token,
		},
		otel: &Otel{
			tracing: &Tracing{
				enabled:  v.GetBool("TRACING_ENABLED"),
				endpoint: v.GetString("TRACING_ENDPOINT"),
			},
		},
		mcp: &Mcp{
			heartbeatEnabled:  v.GetBool("MCP_HEARTBEAT_ENABLED"),
			heartbeatInterval: time.Duration(v.GetInt("MCP_HEARTBEAT_INTERVAL")) * time.Second,
		},
	}

	if strings.ToLower(v.GetString("ENVIRONMENT")) == "dev" {
		s.ginMode = gin.DebugMode
	}

	if err := s.otel.tracing.validate(); err != nil {
		return nil, err
	}

	if err := s.mcp.validate(); err != nil {
		return nil, err
	}

	return s, nil
}
