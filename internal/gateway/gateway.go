package gateway

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/marcoantonios1/costguard/internal/cache"
	"github.com/marcoantonios1/costguard/internal/health"
	"github.com/marcoantonios1/costguard/internal/logging"
	"github.com/marcoantonios1/costguard/internal/notify"
	"github.com/marcoantonios1/costguard/internal/providers"
	"github.com/marcoantonios1/costguard/internal/usage"
)

// HealthRecorder records the outcome of each upstream call for health tracking.
// *health.Tracker satisfies this interface.
type HealthRecorder interface {
	Record(provider string, o health.Outcome)
}

type Router interface {
	PickProvider(model string) string
}

type BudgetChecker interface {
	CheckRequestBudget(ctx context.Context, now time.Time, team string, project string, agent string) error
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
	retryPolicies      map[string]RetryPolicy
	health             HealthRecorder

	audioTranscriptionProvider string
	audioTranscriptionURL      string
	audioTranscriptionModel    string
	audioTTSProvider           string
	audioTTSURL                string
	audioTTSModel              string
}

type Deps struct {
	Router   Router
	Registry *providers.Registry
	Log      *logging.Log

	FallbackProvider      string
	ModelCompatibility    map[string]map[string]string
	Cache                 cache.Cache
	CacheTTL              time.Duration
	UsageStore            usage.Store
	BudgetChecker         BudgetChecker
	AlertStore            AlertStore
	Notifier              Notifier
	ModeToProvider        map[string]string
	ProviderRetryPolicies map[string]RetryPolicy

	AudioTranscriptionProvider string
	AudioTranscriptionURL      string
	AudioTranscriptionModel    string
	AudioTTSProvider           string
	AudioTTSURL                string
	AudioTTSModel              string
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

	retryPolicies := map[string]RetryPolicy{}
	for name, p := range d.ProviderRetryPolicies {
		retryPolicies[name] = p
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
		retryPolicies:      retryPolicies,

		audioTranscriptionProvider: d.AudioTranscriptionProvider,
		audioTranscriptionURL:      d.AudioTranscriptionURL,
		audioTranscriptionModel:    d.AudioTranscriptionModel,
		audioTTSProvider:           d.AudioTTSProvider,
		audioTTSURL:                d.AudioTTSURL,
		audioTTSModel:              d.AudioTTSModel,
	}, nil
}
