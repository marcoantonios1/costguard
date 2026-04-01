package config

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"
)

type Config struct {
	Server    ServerConfig    `json:"server"`
	Logging   LoggingConfig   `json:"logging"`
	Cache     CacheConfig     `json:"cache"`
	Database  DatabaseConfig  `json:"database"`
	Budget    BudgetConfig    `json:"budget"`
	Notify    NotifyConfig    `json:"notify"`
	Reports   ReportsConfig   `json:"reports"`
	Routing   RoutingConfig   `json:"routing"`
	Providers ProvidersConfig `json:"providers"`
	Admin     AdminConfig     `json:"admin"`
}

type DatabaseConfig struct {
	Driver string `json:"driver"`
	DSN    string `json:"dsn"`
}

type ServerConfig struct {
	Addr string `json:"addr"`
}

type LoggingConfig struct {
	Level string `json:"level"`
	JSON  bool   `json:"json"`
}

type CacheConfig struct {
	Enabled bool          `json:"enabled"`
	TTL     time.Duration `json:"ttl"`
	MaxKeys int           `json:"max_keys"`
}

type RoutingConfig struct {
	DefaultProvider    string                       `json:"default_provider"`
	FallbackProvider   string                       `json:"fallback_provider"`
	ModelToProvider    map[string]string            `json:"model_to_provider"`
	ModelCompatibility map[string]map[string]string `json:"model_compatibility"`
	ModeToProvider     map[string]string            `json:"mode_to_provider"`
}

type ProvidersConfig struct {
	OpenAI           map[string]OpenAIProvider           `json:"openai"`
	Anthropic        map[string]AnthropicProvider        `json:"anthropic"`
	Gemini           map[string]GeminiProvider           `json:"gemini"`
	OpenAICompatible map[string]OpenAICompatibleProvider `json:"openai_compatible"`
}

type BudgetConfig struct {
	Enabled    bool               `json:"enabled"`
	MonthlyUSD float64            `json:"monthly_usd"`
	Teams      map[string]float64 `json:"teams"`
	Projects   map[string]float64 `json:"projects"`
	Agents     map[string]float64 `json:"agents"`
}

type NotifyConfig struct {
	Email EmailConfig `json:"email"`
}

type EmailConfig struct {
	Enabled  bool     `json:"enabled"`
	Host     string   `json:"host"`
	Port     int      `json:"port"`
	Username string   `json:"username"`
	Password string   `json:"password"`
	From     string   `json:"from"`
	To       []string `json:"to"`
}

type ReportsConfig struct {
	MonthlyEnabled bool          `json:"monthly_enabled"`
	CheckInterval  time.Duration `json:"check_interval"`
	RunOnStartup   bool          `json:"run_on_startup"`
}

type AdminConfig struct {
	APIKey string `json:"api_key"`
}

type OpenAIProvider struct {
	BaseURL  string           `json:"base_url"`
	APIKey   string           `json:"api_key"`
	Org      string           `json:"org,omitempty"`
	Project  string           `json:"project,omitempty"`
	Timeout  time.Duration    `json:"timeout"`
	Metadata ProviderMetadata `json:"metadata"`
}

type AnthropicProvider struct {
	BaseURL          string           `json:"base_url"`
	APIKey           string           `json:"api_key"`
	AnthropicVersion string           `json:"anthropic_version,omitempty"`
	Timeout          time.Duration    `json:"timeout"`
	Metadata         ProviderMetadata `json:"metadata"`
}

type GeminiProvider struct {
	BaseURL  string           `json:"base_url"`
	APIKey   string           `json:"api_key"`
	Timeout  time.Duration    `json:"timeout"`
	Metadata ProviderMetadata `json:"metadata"`
}

type OpenAICompatibleProvider struct {
	BaseURL  string           `json:"base_url"`
	APIKey   string           `json:"api_key"`
	Timeout  time.Duration    `json:"timeout"`
	Metadata ProviderMetadata `json:"metadata"`
}

type ProviderMetadata struct {
	Kind               string   `json:"kind"`
	SupportsTools      bool     `json:"supports_tools"`
	SupportsStreaming  bool     `json:"supports_streaming"`
	SupportsVision     bool     `json:"supports_vision"`
	SupportsEmbeddings bool     `json:"supports_embeddings"`
	Priority           int      `json:"priority"`
	Tags               []string `json:"tags"`
}

type rawProviderMetadata struct {
	Kind               string   `json:"kind"`
	SupportsTools      bool     `json:"supports_tools"`
	SupportsStreaming  bool     `json:"supports_streaming"`
	SupportsVision     bool     `json:"supports_vision"`
	SupportsEmbeddings bool     `json:"supports_embeddings"`
	Priority           int      `json:"priority"`
	Tags               []string `json:"tags"`
}

func Load(path string) (Config, error) {
	var c Config

	b, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}

	type rawCache struct {
		Enabled bool   `json:"enabled"`
		TTL     string `json:"ttl"`
		MaxKeys int    `json:"max_keys"`
	}

	type rawOpenAIProvider struct {
		BaseURL  string              `json:"base_url"`
		APIKey   string              `json:"api_key"`
		Org      string              `json:"org,omitempty"`
		Project  string              `json:"project,omitempty"`
		Timeout  string              `json:"timeout"`
		Metadata rawProviderMetadata `json:"metadata"`
	}

	type rawAnthropicProvider struct {
		BaseURL          string              `json:"base_url"`
		APIKey           string              `json:"api_key"`
		AnthropicVersion string              `json:"anthropic_version,omitempty"`
		Timeout          string              `json:"timeout"`
		Metadata         rawProviderMetadata `json:"metadata"`
	}

	type rawGeminiProvider struct {
		BaseURL  string              `json:"base_url"`
		APIKey   string              `json:"api_key"`
		Timeout  string              `json:"timeout"`
		Metadata rawProviderMetadata `json:"metadata"`
	}

	type rawOpenAICompatibleProvider struct {
		BaseURL  string              `json:"base_url"`
		APIKey   string              `json:"api_key"`
		Timeout  string              `json:"timeout"`
		Metadata rawProviderMetadata `json:"metadata"`
	}

	type rawReports struct {
		MonthlyEnabled bool   `json:"monthly_enabled"`
		CheckInterval  string `json:"check_interval"`
		RunOnStartup   bool   `json:"run_on_startup"`
	}

	type rawConfig struct {
		Server    ServerConfig   `json:"server"`
		Logging   LoggingConfig  `json:"logging"`
		Cache     rawCache       `json:"cache"`
		Database  DatabaseConfig `json:"database"`
		Budget    BudgetConfig   `json:"budget"`
		Notify    NotifyConfig   `json:"notify"`
		Reports   rawReports     `json:"reports"`
		Admin     AdminConfig    `json:"admin"`
		Routing   RoutingConfig  `json:"routing"`
		Providers struct {
			OpenAI           map[string]rawOpenAIProvider           `json:"openai"`
			Anthropic        map[string]rawAnthropicProvider        `json:"anthropic"`
			Gemini           map[string]rawGeminiProvider           `json:"gemini"`
			OpenAICompatible map[string]rawOpenAICompatibleProvider `json:"openai_compatible"`
		} `json:"providers"`
	}

	var rc rawConfig
	if err := json.Unmarshal(b, &rc); err != nil {
		return c, err
	}

	c.Server = rc.Server
	c.Logging = rc.Logging
	c.Routing = rc.Routing

	c.Cache.Enabled = rc.Cache.Enabled
	c.Cache.MaxKeys = rc.Cache.MaxKeys

	c.Database = rc.Database
	c.Database.DSN = resolveEnv(rc.Database.DSN)

	c.Budget = rc.Budget
	c.Notify = rc.Notify

	c.Admin = rc.Admin
	c.Admin.APIKey = resolveEnv(rc.Admin.APIKey)

	c.Notify.Email.Username = resolveEnv(rc.Notify.Email.Username)
	c.Notify.Email.Password = resolveEnv(rc.Notify.Email.Password)
	c.Notify.Email.From = resolveEnv(rc.Notify.Email.From)
	c.Notify.Email.To = make([]string, len(rc.Notify.Email.To))
	for i, v := range rc.Notify.Email.To {
		c.Notify.Email.To[i] = resolveEnv(v)
	}

	if c.Notify.Email.Enabled {
		if c.Notify.Email.Host == "" {
			return c, errors.New("notify.email.host is required when email notifications are enabled")
		}
		if c.Notify.Email.Port <= 0 {
			return c, errors.New("notify.email.port must be greater than 0 when email notifications are enabled")
		}
		if c.Notify.Email.Username == "" {
			return c, errors.New("notify.email.username is required when email notifications are enabled")
		}
		if c.Notify.Email.Password == "" {
			return c, errors.New("notify.email.password is required when email notifications are enabled")
		}
		if c.Notify.Email.From == "" {
			return c, errors.New("notify.email.from is required when email notifications are enabled")
		}
		if len(c.Notify.Email.To) == 0 {
			return c, errors.New("notify.email.to must contain at least one recipient when email notifications are enabled")
		}
	}

	if rc.Cache.TTL != "" {
		d, err := time.ParseDuration(rc.Cache.TTL)
		if err != nil {
			return c, err
		}
		c.Cache.TTL = d
	}

	c.Reports.MonthlyEnabled = rc.Reports.MonthlyEnabled
	c.Reports.RunOnStartup = rc.Reports.RunOnStartup
	if rc.Reports.CheckInterval != "" {
		d, err := time.ParseDuration(rc.Reports.CheckInterval)
		if err != nil {
			return c, err
		}
		c.Reports.CheckInterval = d
	}

	c.Providers.OpenAI = map[string]OpenAIProvider{}
	for name, p := range rc.Providers.OpenAI {
		var to time.Duration
		if p.Timeout != "" {
			d, err := time.ParseDuration(p.Timeout)
			if err != nil {
				return c, err
			}
			to = d
		}

		c.Providers.OpenAI[name] = OpenAIProvider{
			BaseURL:  p.BaseURL,
			APIKey:   resolveEnvIfPresent(p.APIKey),
			Org:      p.Org,
			Project:  p.Project,
			Timeout:  to,
			Metadata: normalizeProviderMetadata(p.Metadata),
		}
	}

	c.Providers.Anthropic = map[string]AnthropicProvider{}
	for name, p := range rc.Providers.Anthropic {
		var to time.Duration
		if p.Timeout != "" {
			d, err := time.ParseDuration(p.Timeout)
			if err != nil {
				return c, err
			}
			to = d
		}

		c.Providers.Anthropic[name] = AnthropicProvider{
			BaseURL:          p.BaseURL,
			APIKey:           resolveEnvIfPresent(p.APIKey),
			AnthropicVersion: p.AnthropicVersion,
			Timeout:          to,
			Metadata:         normalizeProviderMetadata(p.Metadata),
		}
	}

	c.Providers.Gemini = map[string]GeminiProvider{}
	for name, p := range rc.Providers.Gemini {
		var to time.Duration
		if p.Timeout != "" {
			d, err := time.ParseDuration(p.Timeout)
			if err != nil {
				return c, err
			}
			to = d
		}

		c.Providers.Gemini[name] = GeminiProvider{
			BaseURL:  p.BaseURL,
			APIKey:   resolveEnvIfPresent(p.APIKey),
			Timeout:  to,
			Metadata: normalizeProviderMetadata(p.Metadata),
		}
	}

	c.Providers.OpenAICompatible = map[string]OpenAICompatibleProvider{}
	for name, p := range rc.Providers.OpenAICompatible {
		var to time.Duration
		if p.Timeout != "" {
			d, err := time.ParseDuration(p.Timeout)
			if err != nil {
				return c, err
			}
			to = d
		}

		c.Providers.OpenAICompatible[name] = OpenAICompatibleProvider{
			BaseURL:  p.BaseURL,
			APIKey:   resolveEnvIfPresent(p.APIKey), // optional
			Timeout:  to,
			Metadata: normalizeProviderMetadata(p.Metadata),
		}
	}

	if c.Server.Addr == "" {
		c.Server.Addr = ":8080"
	}
	if c.Routing.DefaultProvider == "" {
		return c, errors.New("routing.default_provider is required")
	}
	if !hasUsableProvider(c) {
		return c, errors.New("at least one usable provider must be configured")
	}

	return c, nil
}

func resolveEnv(val string) string {
	if strings.HasPrefix(val, "env:") {
		key := strings.TrimPrefix(val, "env:")
		env := os.Getenv(key)
		if env == "" {
			panic("missing required env: " + key)
		}
		return env
	}
	return val
}

func resolveEnvIfPresent(val string) string {
	if strings.HasPrefix(val, "env:") {
		key := strings.TrimPrefix(val, "env:")
		return os.Getenv(key)
	}
	return val
}

func hasUsableProvider(c Config) bool {
	return hasUsableOpenAI(c) ||
		hasUsableAnthropic(c) ||
		hasUsableGemini(c) ||
		hasUsableOpenAICompatible(c)
}

func hasUsableOpenAI(c Config) bool {
	for _, p := range c.Providers.OpenAI {
		if strings.TrimSpace(p.BaseURL) != "" && strings.TrimSpace(p.APIKey) != "" {
			return true
		}
	}
	return false
}

func hasUsableAnthropic(c Config) bool {
	for _, p := range c.Providers.Anthropic {
		if strings.TrimSpace(p.BaseURL) != "" && strings.TrimSpace(p.APIKey) != "" {
			return true
		}
	}
	return false
}

func hasUsableGemini(c Config) bool {
	for _, p := range c.Providers.Gemini {
		if strings.TrimSpace(p.BaseURL) != "" && strings.TrimSpace(p.APIKey) != "" {
			return true
		}
	}
	return false
}

func hasUsableOpenAICompatible(c Config) bool {
	for _, p := range c.Providers.OpenAICompatible {
		if strings.TrimSpace(p.BaseURL) != "" {
			return true
		}
	}
	return false
}

func normalizeProviderMetadata(in rawProviderMetadata) ProviderMetadata {
	kind := strings.TrimSpace(strings.ToLower(in.Kind))
	switch kind {
	case "cloud", "local":
	default:
		kind = ""
	}

	out := ProviderMetadata{
		Kind:               kind,
		SupportsTools:      in.SupportsTools,
		SupportsStreaming:  in.SupportsStreaming,
		SupportsVision:     in.SupportsVision,
		SupportsEmbeddings: in.SupportsEmbeddings,
		Priority:           in.Priority,
		Tags:               append([]string(nil), in.Tags...),
	}

	return out
}
