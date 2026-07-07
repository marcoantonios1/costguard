package feedback

import "time"

// FeedbackRecord captures a single feedback event submitted by a consumer.
type FeedbackRecord struct {
	Consumer        string    `json:"consumer"`
	Role            string    `json:"role"`
	Model           string    `json:"model"`
	TaskFingerprint string    `json:"task_fingerprint"`
	Outcome         string    `json:"outcome"` // "success" | "failure"
	ReviewerPassed  bool      `json:"reviewer_passed"`
	ReviewerRetries int       `json:"reviewer_retries"`
	UserAccepted    bool      `json:"user_accepted"`
	InputTokens     int       `json:"input_tokens"`
	OutputTokens    int       `json:"output_tokens"`
	DurationMS      int       `json:"duration_ms"`
	Timestamp       time.Time `json:"timestamp"`
	ReceivedAt      time.Time `json:"received_at"` // set server-side on ingestion
}
