package config

import (
	"encoding/json"
	"errors"
	"os"
	"time"
)

type Config struct {
	Server    ServerConfig    `json:"server"`
	Logging   LoggingConfig   `json:"logging"`
	Cache     CacheConfig     `json:"cache"`
	Database  DatabaseConfig  `json:"database"`
	Budget    BudgetConfig    `json:"budget"`
	Routing   RoutingConfig   `json:"routing"`
	Providers ProvidersConfig `json:"providers"`
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
	Enabled    bool    `json:"enabled"`
	MonthlyUSD float64 `json:"monthly_usd"`
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
	type rawConfig struct {
		Server    ServerConfig   `json:"server"`
		Logging   LoggingConfig  `json:"logging"`
		Cache     rawCache       `json:"cache"`
		Database  DatabaseConfig `json:"database"`
		Budget    BudgetConfig   `json:"budget"`
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
	c.Database.DSN = rc.Database.DSN
	c.Budget = rc.Budget
	if rc.Cache.TTL != "" {
		d, err := time.ParseDuration(rc.Cache.TTL)
		if err != nil {
			return c, err
		}
		c.Cache.TTL = d
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
			APIKey:  p.APIKey,
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
