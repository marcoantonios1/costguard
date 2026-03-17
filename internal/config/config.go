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
	Addr string `json:"addr"` // e.g. ":8080"
}

type LoggingConfig struct {
	Level string `json:"level"` // "debug"|"info"|"warn"|"error"
	JSON  bool   `json:"json"`
}

type CacheConfig struct {
	Enabled bool          `json:"enabled"`
	TTL     time.Duration `json:"ttl"` // parsed from string in Load()
	MaxKeys int           `json:"max_keys"`
}

type RoutingConfig struct {
	DefaultProvider  string            `json:"default_provider"`
	FallbackProvider string            `json:"fallback_provider"`
	ModelToProvider  map[string]string `json:"model_to_provider"`
}

type ProvidersConfig struct {
	OpenAI map[string]OpenAIProvider `json:"openai"` // named instances
}

type BudgetConfig struct {
	Enabled    bool               `json:"enabled"`
	MonthlyUSD float64            `json:"monthly_usd"`
	Teams      map[string]float64 `json:"teams"`
	Projects   map[string]float64 `json:"projects"`
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
	BaseURL string        `json:"base_url"` // default https://api.openai.com
	APIKey  string        `json:"api_key"`
	Org     string        `json:"org,omitempty"`
	Project string        `json:"project,omitempty"`
	Timeout time.Duration `json:"timeout"`
}

func Load(path string) (Config, error) {
	var c Config
	b, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}

	// Decode with durations as strings by using a shadow type
	type rawCache struct {
		Enabled bool   `json:"enabled"`
		TTL     string `json:"ttl"`
		MaxKeys int    `json:"max_keys"`
	}
	type rawOpenAIProvider struct {
		BaseURL string `json:"base_url"`
		APIKey  string `json:"api_key"`
		Org     string `json:"org,omitempty"`
		Project string `json:"project,omitempty"`
		Timeout string `json:"timeout"`
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
		Routing   RoutingConfig  `json:"routing"`
		Providers struct {
			OpenAI map[string]rawOpenAIProvider `json:"openai"`
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
			BaseURL: p.BaseURL,
			APIKey:  resolveEnv(p.APIKey),
			Org:     p.Org,
			Project: p.Project,
			Timeout: to,
		}
	}

	if c.Server.Addr == "" {
		c.Server.Addr = ":8080"
	}
	if c.Routing.DefaultProvider == "" {
		return c, errors.New("routing.default_provider is required")
	}

	return c, nil
}

func resolveEnv(value string) string {
	value = strings.TrimSpace(value)

	const prefix = "env:"
	if strings.HasPrefix(value, prefix) {
		key := strings.TrimSpace(strings.TrimPrefix(value, prefix))
		return strings.TrimSpace(os.Getenv(key))
	}

	return value
}
