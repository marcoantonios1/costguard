package report

import (
	"fmt"
	"strings"
)

func FormatMonthlySummaryEmail(summary MonthlySummary) (string, string) {
	subject := fmt.Sprintf(
		"[Costguard] Monthly usage report (%s)",
		summary.From.Format("2006-01"),
	)

	var b strings.Builder

	b.WriteString("Costguard monthly usage report\n\n")
	b.WriteString(fmt.Sprintf("Period: %s to %s\n", summary.From.Format("2006-01-02"), summary.To.Format("2006-01-02")))
	b.WriteString(fmt.Sprintf("Total spend: $%.6f\n\n", summary.TotalSpend))

	b.WriteString("Spend by team:\n")
	if len(summary.ByTeam) == 0 {
		b.WriteString("- none\n")
	} else {
		for _, item := range summary.ByTeam {
			name := item.Team
			if name == "" {
				name = "(none)"
			}
			b.WriteString(fmt.Sprintf("- %s: $%.6f\n", name, item.Spend))
		}
	}
	b.WriteString("\n")

	b.WriteString("Spend by provider:\n")
	if len(summary.ByProvider) == 0 {
		b.WriteString("- none\n")
	} else {
		for _, item := range summary.ByProvider {
			name := item.Provider
			if name == "" {
				name = "(none)"
			}
			b.WriteString(fmt.Sprintf("- %s: $%.6f\n", name, item.Spend))
		}
	}
	b.WriteString("\n")

	b.WriteString("Spend by model:\n")
	if len(summary.ByModel) == 0 {
		b.WriteString("- none\n")
	} else {
		for _, item := range summary.ByModel {
			name := item.Model
			if name == "" {
				name = "(none)"
			}
			b.WriteString(fmt.Sprintf("- %s: $%.6f\n", name, item.Spend))
		}
	}

	return subject, b.String()
}