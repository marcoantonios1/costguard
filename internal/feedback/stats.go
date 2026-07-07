package feedback

// ModelStats summarizes feedback outcomes over a set of records.
type ModelStats struct {
	Consumer           string  `json:"consumer,omitempty"`
	Role               string  `json:"role,omitempty"`
	Model              string  `json:"model,omitempty"`
	TaskCount          int     `json:"task_count"`
	SuccessRate        float64 `json:"success_rate"`
	ReviewerPassRate   float64 `json:"reviewer_pass_rate"`
	AvgReviewerRetries float64 `json:"avg_reviewer_retries"`
	AvgDurationMS      float64 `json:"avg_duration_ms"`
	AvgInputTokens     float64 `json:"avg_input_tokens"`
	AvgOutputTokens    float64 `json:"avg_output_tokens"`
}

// Aggregate computes ModelStats over records. Consumer/Role/Model on the
// returned struct are taken from f so callers know what the stats are for.
func Aggregate(records []FeedbackRecord, f Filter) ModelStats {
	if len(records) == 0 {
		return ModelStats{Consumer: f.Consumer, Role: f.Role, Model: f.Model}
	}

	n := len(records)
	var successes, reviewerPasses, retries, durationSum, inputSum, outputSum int
	for _, r := range records {
		if r.Outcome == "success" {
			successes++
		}
		if r.ReviewerPassed {
			reviewerPasses++
		}
		retries += r.ReviewerRetries
		durationSum += r.DurationMS
		inputSum += r.InputTokens
		outputSum += r.OutputTokens
	}

	fn := float64(n)
	return ModelStats{
		Consumer:           f.Consumer,
		Role:               f.Role,
		Model:              f.Model,
		TaskCount:          n,
		SuccessRate:        float64(successes) / fn,
		ReviewerPassRate:   float64(reviewerPasses) / fn,
		AvgReviewerRetries: float64(retries) / fn,
		AvgDurationMS:      float64(durationSum) / fn,
		AvgInputTokens:     float64(inputSum) / fn,
		AvgOutputTokens:    float64(outputSum) / fn,
	}
}
