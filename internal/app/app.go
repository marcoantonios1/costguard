package app

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/marcoantonios1/costguard/internal/alert"
	"github.com/marcoantonios1/costguard/internal/budget"
	"github.com/marcoantonios1/costguard/internal/cache"
	"github.com/marcoantonios1/costguard/internal/config"
	"github.com/marcoantonios1/costguard/internal/database"
	"github.com/marcoantonios1/costguard/internal/gateway"
	"github.com/marcoantonios1/costguard/internal/logging"
	"github.com/marcoantonios1/costguard/internal/notify"
	"github.com/marcoantonios1/costguard/internal/providers"
	anthropic_provider "github.com/marcoantonios1/costguard/internal/providers/anthropic"
	gemini_provider "github.com/marcoantonios1/costguard/internal/providers/gemini"
	openai_provider "github.com/marcoantonios1/costguard/internal/providers/openai"
	openaicompat_provider "github.com/marcoantonios1/costguard/internal/providers/openaicompat"
	"github.com/marcoantonios1/costguard/internal/report"
	"github.com/marcoantonios1/costguard/internal/router"
	"github.com/marcoantonios1/costguard/internal/server"
	"github.com/marcoantonios1/costguard/internal/server/admin"
	openai_http "github.com/marcoantonios1/costguard/internal/server/openai"
	"github.com/marcoantonios1/costguard/internal/usage"
)

type App struct {
	cfg             config.Config
	log             *logging.Log
	server          *server.Server
	reportScheduler *report.Scheduler
}

func New(cfg config.Config, log *logging.Log) (*App, error) {
	reg := providers.NewRegistry()
	availableProviders := map[string]bool{}

	for name, p := range cfg.Providers.OpenAI {
		if p.BaseURL == "" {
			log.Info("skip_provider_missing_base_url", map[string]any{
				"name":          name,
				"type":          "openai",
				"auth_required": true,
			})
			continue
		}

		if p.APIKey == "" {
			log.Info("skip_provider_missing_api_key", map[string]any{
				"name":          name,
				"type":          "openai",
				"auth_required": true,
			})
			continue
		}

		adapter, err := openai_provider.NewClient(openai_provider.ClientConfig{
			Name:    name,
			BaseURL: p.BaseURL,
			APIKey:  p.APIKey,
			Org:     p.Org,
			Project: p.Project,
			Timeout: p.Timeout,
		})
		if err != nil {
			log.Error("failed_to_create_openai_client", map[string]any{
				"name":  name,
				"type":  "openai",
				"error": err,
			})
			return nil, err
		}

		reg.Register(name, adapter)
		availableProviders[name] = true

		log.Info("provider_enabled", map[string]any{
			"name":          name,
			"type":          "openai",
			"auth_required": true,
			"base_url":      p.BaseURL,
		})
	}

	for name, p := range cfg.Providers.Anthropic {
		if p.BaseURL == "" {
			log.Info("skip_provider_missing_base_url", map[string]any{
				"name":          name,
				"type":          "anthropic",
				"auth_required": true,
			})
			continue
		}

		if p.APIKey == "" {
			log.Info("skip_provider_missing_api_key", map[string]any{
				"name":          name,
				"type":          "anthropic",
				"auth_required": true,
			})
			continue
		}

		adapter, err := anthropic_provider.NewClient(anthropic_provider.ClientConfig{
			Name:             name,
			BaseURL:          p.BaseURL,
			APIKey:           p.APIKey,
			AnthropicVersion: p.AnthropicVersion,
			Timeout:          p.Timeout,
		})
		if err != nil {
			log.Error("failed_to_create_anthropic_client", map[string]any{
				"name":  name,
				"type":  "anthropic",
				"error": err,
			})
			return nil, err
		}

		reg.Register(name, adapter)
		availableProviders[name] = true

		log.Info("provider_enabled", map[string]any{
			"name":          name,
			"type":          "anthropic",
			"auth_required": true,
			"base_url":      p.BaseURL,
		})
	}

	for name, p := range cfg.Providers.Gemini {
		if p.BaseURL == "" {
			log.Info("skip_provider_missing_base_url", map[string]any{
				"name":          name,
				"type":          "gemini",
				"auth_required": true,
			})
			continue
		}

		if p.APIKey == "" {
			log.Info("skip_provider_missing_api_key", map[string]any{
				"name":          name,
				"type":          "gemini",
				"auth_required": true,
			})
			continue
		}

		adapter, err := gemini_provider.NewClient(gemini_provider.ClientConfig{
			Name:    name,
			BaseURL: p.BaseURL,
			APIKey:  p.APIKey,
			Timeout: p.Timeout,
		})
		if err != nil {
			log.Error("failed_to_create_gemini_client", map[string]any{
				"name":  name,
				"type":  "gemini",
				"error": err,
			})
			return nil, err
		}

		reg.Register(name, adapter)
		availableProviders[name] = true

		log.Info("provider_enabled", map[string]any{
			"name":          name,
			"type":          "gemini",
			"auth_required": true,
			"base_url":      p.BaseURL,
		})
	}

	for name, p := range cfg.Providers.OpenAICompatible {
		if p.BaseURL == "" {
			log.Info("skip_provider_missing_base_url", map[string]any{
				"name":          name,
				"type":          "openai_compatible",
				"auth_required": false,
			})
			continue
		}

		adapter, err := openaicompat_provider.NewClient(openaicompat_provider.ClientConfig{
			Name:    name,
			BaseURL: p.BaseURL,
			APIKey:  p.APIKey,
			Timeout: p.Timeout,
		})
		if err != nil {
			log.Error("failed_to_create_openai_compatible_client", map[string]any{
				"name":  name,
				"type":  "openai_compatible",
				"error": err,
			})
			return nil, err
		}

		reg.Register(name, adapter)
		availableProviders[name] = true

		log.Info("provider_enabled", map[string]any{
			"name":          name,
			"type":          "openai_compatible",
			"auth_required": false,
			"base_url":      p.BaseURL,
			"has_api_key":   strings.TrimSpace(p.APIKey) != "",
		})
	}

	rt := router.New(router.Config{
		DefaultProvider:    cfg.Routing.DefaultProvider,
		ModelToProvider:    cfg.Routing.ModelToProvider,
		AvailableProviders: availableProviders,
		Log:                log,
	})

	var c cache.Cache
	if cfg.Cache.Enabled {
		c = cache.NewMemory(cfg.Cache.MaxKeys)
	}

	ctx := context.Background()
	pool, err := database.NewPostgresPool(ctx, cfg.Database.DSN)
	if err != nil {
		return nil, err
	}

	alertStore := alert.NewPostgresStore(pool)
	usageStore := usage.NewPostgresStore(pool)

	budgetSvc := budget.NewService(usageStore, budget.Config{
		Enabled:    cfg.Budget.Enabled,
		MonthlyUSD: cfg.Budget.MonthlyUSD,
		Teams:      cfg.Budget.Teams,
		Projects:   cfg.Budget.Projects,
	})

	var notifier notify.Sender
	if cfg.Notify.Email.Enabled {
		notifier = notify.NewSMTPSender(cfg.Notify.Email)
	} else {
		notifier = notify.NewLogSender(log)
	}

	reportSvc := report.NewService(usageStore)
	reportDeliveryStore := report.NewPostgresDeliveryStore(pool)
	reportEmailSvc := report.NewEmailService(reportSvc, notifier, reportDeliveryStore)

	reportScheduler := report.NewScheduler(reportEmailSvc, log, report.SchedulerConfig{
		Enabled:       cfg.Reports.MonthlyEnabled,
		CheckInterval: cfg.Reports.CheckInterval,
		RunOnStartup:  cfg.Reports.RunOnStartup,
	})

	gw, err := gateway.New(gateway.Deps{
		Router:             rt,
		Registry:           reg,
		Log:                log,
		FallbackProvider:   cfg.Routing.FallbackProvider,
		ModelCompatibility: cfg.Routing.ModelCompatibility,
		Cache:              c,
		CacheTTL:           cfg.Cache.TTL,
		UsageStore:         usageStore,
		BudgetChecker:      budgetSvc,
		AlertStore:         alertStore,
		Notifier:           notifier,
	})
	if err != nil {
		log.Error("failed_to_create_gateway", map[string]any{"error": err})
		return nil, err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", server.HealthzHandler())

	openai_http.Register(mux, openai_http.Deps{Gateway: gw})

	adminMux := http.NewServeMux()
	admin.Register(adminMux, admin.Deps{
		UsageStore: usageStore,
		Reports:    reportEmailSvc,
		Log:        log,
		Budget:     budgetSvc,
	})

	protectedAdmin := server.AdminAuth(cfg.Admin.APIKey)(adminMux)
	mux.Handle("/admin/", http.StripPrefix("/admin", protectedAdmin))

	handler := server.LoggingMiddleware(log, mux)

	srv := server.NewServer(server.Deps{
		Addr:    cfg.Server.Addr,
		Handler: handler,
	})

	return &App{
		cfg:             cfg,
		log:             log,
		server:          srv,
		reportScheduler: reportScheduler,
	}, nil
}

func (a *App) Run() error {
	if a.server == nil {
		return errors.New("server is nil")
	}

	appCtx, appCancel := context.WithCancel(context.Background())
	defer appCancel()

	if a.reportScheduler != nil {
		a.reportScheduler.Start(appCtx)
	}

	errCh := make(chan error, 1)
	go func() {
		a.log.Info("server_start", map[string]any{"addr": a.cfg.Server.Addr})
		errCh <- a.server.ListenAndServe()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		a.log.Info("shutdown_signal", map[string]any{"signal": sig.String()})
		appCancel()

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return a.server.Shutdown(ctx)

	case err := <-errCh:
		appCancel()
		a.log.Error("server_error", map[string]any{"error": err})
		return err
	}
}
