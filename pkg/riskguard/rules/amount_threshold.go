package rules

import (
	"context"
	"fmt"

	"github.com/peymanahmadi/riskguard/pkg/riskguard"
)

// AmountThresholdRule flags transactions whose amount exceeds a configured
// threshold (in minor currency units), scaling the score with how far past
// the threshold the amount is rather than a hard cliff.
type AmountThresholdRule struct {
	ThresholdMinor int64
	Currency       string
	// SoftMultiple is how many multiples of ThresholdMinor it takes to reach
	// a score of 100. Defaults to 5 if zero (i.e. 5x the threshold maxes the
	// rule out).
	SoftMultiple float64
}

func NewAmountThresholdRule(thresholdMinor int64, currency string) *AmountThresholdRule {
	return &AmountThresholdRule{ThresholdMinor: thresholdMinor, Currency: currency}
}

func (r *AmountThresholdRule) Name() string { return "amount_threshold" }

func (r *AmountThresholdRule) Evaluate(_ context.Context, tx riskguard.Transaction) (riskguard.RuleResult, error) {
	if tx.Currency != r.Currency {
		// This rule only judges amounts in its configured currency; compose
		// multiple instances (or add FX conversion upstream) for multi
		// currency support.
		return riskguard.RuleResult{Rule: r.Name(), Score: 0, Triggered: false}, nil
	}

	if tx.AmountMinor <= r.ThresholdMinor {
		return riskguard.RuleResult{Rule: r.Name(), Score: 0, Triggered: false}, nil
	}

	softMultiple := r.SoftMultiple
	if softMultiple == 0 {
		softMultiple = 5
	}

	over := float64(tx.AmountMinor-r.ThresholdMinor) / float64(r.ThresholdMinor)
	score := (over / softMultiple) * 100
	if score > 100 {
		score = 100
	}

	severity := riskguard.SeverityLow
	if score >= 50 {
		severity = riskguard.SeverityMedium
	}
	if score >= 85 {
		severity = riskguard.SeverityHigh
	}

	return riskguard.RuleResult{
		Rule:      r.Name(),
		Triggered: true,
		Score:     score,
		Severity:  severity,
		Reason: fmt.Sprintf("amount %d %s exceeds threshold %d %s",
			tx.AmountMinor, tx.Currency, r.ThresholdMinor, r.Currency),
	}, nil
}
