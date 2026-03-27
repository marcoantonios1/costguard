package gateway

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/marcoantonios1/costguard/internal/cache"
	"github.com/marcoantonios1/costguard/internal/logging"
	"github.com/marcoantonios1/costguard/internal/notify"
	"github.com/marcoantonios1/costguard/internal/providers"
	"github.com/marcoantonios1/costguard/internal/usage"
)

type Router interface {
	PickProvider(model string) string
}

type BudgetChecker interface {
	CheckRequestBudget(ctx context.Context, now time.Time, team string, project string) error
}

type AlertStore interface {
	WasSent(ctx context.Context, periodStart time.Time, thresholdPercent int, alertType string) (bool, error)
	MarkSent(ctx context.Context, periodStart time.Time, thresholdPercent int, alertType string) error
}

type Notifier interface {
	Send(ctx context.Context, msg notify.Message) error
}

type Gateway struct {
	router Router
	reg    *providers.Registry
	log    *logging.Log

	fallback           string
	modelCompatibility map[string]map[string]string
	cache              cache.Cache
	cacheTTL           time.Duration
	usageStore         usage.Store
	budgetChecker      BudgetChecker
	alertStore         AlertStore
	notifier           Notifier
	modeToProvider     map[string]string
}

type Deps struct {
	Router   Router
	Registry *providers.Registry
	Log      *logging.Log

	FallbackProvider   string
	ModelCompatibility map[string]map[string]string
	Cache              cache.Cache
	CacheTTL           time.Duration
	UsageStore         usage.Store
	BudgetChecker      BudgetChecker
	AlertStore         AlertStore
	Notifier           Notifier
	ModeToProvider     map[string]string
}

type openAIUsageResponse struct {
	Model string `json:"model"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

func New(d Deps) (*Gateway, error) {
	if d.Router == nil {
		return nil, errors.New("router is required")
	}
	if d.Registry == nil {
		return nil, errors.New("registry is required")
	}

	modeMap := map[string]string{}
	for k, v := range d.ModeToProvider {
		modeMap[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
	}

	return &Gateway{
		router:             d.Router,
		reg:                d.Registry,
		log:                d.Log,
		fallback:           d.FallbackProvider,
		modelCompatibility: d.ModelCompatibility,
		cache:              d.Cache,
		cacheTTL:           d.CacheTTL,
		usageStore:         d.UsageStore,
		budgetChecker:      d.BudgetChecker,
		alertStore:         d.AlertStore,
		notifier:           d.Notifier,
		modeToProvider:     modeMap,
	}, nil
}
