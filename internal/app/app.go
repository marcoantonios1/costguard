package app

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
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
	openai_provider "github.com/marcoantonios1/costguard/internal/providers/openai"
	"github.com/marcoantonios1/costguard/internal/report"
	"github.com/marcoantonios1/costguard/internal/router"
	"github.com/marcoantonios1/costguard/internal/server"
	"github.com/marcoantonios1/costguard/internal/server/admin"
	openai_http "github.com/marcoantonios1/costguard/internal/server/openai"
	"github.com/marcoantonios1/costguard/internal/usage"
)

type App struct {
	cfg    config.Config
	log    *logging.Log
	server *server.Server
}

func New(cfg config.Config, log *logging.Log) (*App, error) {
	// Provider registry
	reg := providers.NewRegistry()

	// Register OpenAI provider instances from config
	for name, p := range cfg.Providers.OpenAI {
		adapter, err := openai_provider.NewClient(openai_provider.ClientConfig{
			Name:    name,
			BaseURL: p.BaseURL,
			APIKey:  p.APIKey,
			Org:     p.Org,
			Project: p.Project,
			Timeout: p.Timeout,
		})
		if err != nil {
			log.Error("failed_to_create_openai_client", map[string]any{"name": name, "error": err})
			return nil, err
		}
		reg.Register(name, adapter)
	}

	// Router
	rt := router.New(router.Config{
		DefaultProvider: cfg.Routing.DefaultProvider,
		ModelToProvider: cfg.Routing.ModelToProvider,
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
	})

	var notifier notify.Sender
	if cfg.Notify.Email.Enabled {
		notifier = notify.NewSMTPSender(cfg.Notify.Email)
	} else {
		notifier = notify.NewLogSender(log)
	}

	reportSvc := report.NewService(usageStore)
	reportEmailSvc := report.NewEmailService(reportSvc, notifier)

	gw, err := gateway.New(gateway.Deps{
		Router:           rt,
		Registry:         reg,
		Log:              log,
		FallbackProvider: cfg.Routing.FallbackProvider,
		Cache:            c,
		CacheTTL:         cfg.Cache.TTL,
		UsageStore:       usageStore,
		BudgetChecker:    budgetSvc,
		AlertStore:       alertStore,
		Notifier:         notifier,
	})
	if err != nil {
		log.Error("failed_to_create_gateway", map[string]any{"error": err})
		return nil, err
	}

	// HTTP handlers (Phase A: only healthz now; OpenAI proxy added later in step 5/6)
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", server.HealthzHandler())

	openai_http.Register(mux, openai_http.Deps{Gateway: gw})

	admin.Register(mux, admin.Deps{
		UsageStore: usageStore,
		Reports:    reportEmailSvc,
		Log:        log,
	})

	// wrap middleware
	handler := server.LoggingMiddleware(log, mux)

	srv := server.NewServer(server.Deps{
		Addr:    cfg.Server.Addr,
		Handler: handler,
	})

	_ = rt // will be used once openai_proxy handler is added

	return &App{cfg: cfg, log: log, server: srv}, nil
}

func (a *App) Run() error {
	if a.server == nil {
		return errors.New("server is nil")
	}

	// graceful shutdown
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
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return a.server.Shutdown(ctx)
	case err := <-errCh:
		a.log.Error("server_error", map[string]any{"error": err})
		return err
	}
}
