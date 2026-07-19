// Package rules contains concrete, production-oriented riskguard.Rule
// implementations. Each rule is self-contained and depends only on the
// small storage interfaces it actually needs, so callers can mix Postgres,
// Redis, and in-memory backends freely.
package rules

import (
	"context"
	"fmt"
	"time"

	"github.com/peymanahmadi/riskguard/pkg/riskguard"
)

// VelocityRule flags entities transacting more than MaxCount times within
// Window. This is the classic "card testing" / bot-abuse detector: a
// compromised account or stolen card is often used for a rapid burst of
// small transactions.
type VelocityRule struct {
	Store    riskguard.CounterStore
	Window   time.Duration
	MaxCount int64
	// ScorePerOverage controls how quickly the score ramps up once MaxCount
	// is exceeded. Defaults to 15 if zero.
	ScorePerOverage float64
}

func NewVelocityRule(store riskguard.CounterStore, window time.Duration, maxCount int64) *VelocityRule {
	return &VelocityRule{Store: store, Window: window, MaxCount: maxCount}
}

func (r *VelocityRule) Name() string { return "velocity" }

func (r *VelocityRule) Evaluate(ctx context.Context, tx riskguard.Transaction) (riskguard.RuleResult, error) {
	key := "velocity:" + tx.EntityID
	count, err := r.Store.Increment(ctx, key, r.Window)
	if err != nil {
		return riskguard.RuleResult{}, fmt.Errorf("velocity: increment counter: %w", err)
	}

	if count <= r.MaxCount {
		return riskguard.RuleResult{Rule: r.Name(), Score: 0, Triggered: false}, nil
	}

	perOverage := r.ScorePerOverage
	if perOverage == 0 {
		perOverage = 15
	}
	overage := float64(count - r.MaxCount)
	score := overage * perOverage
	if score > 100 {
		score = 100
	}

	severity := riskguard.SeverityMedium
	if score >= 60 {
		severity = riskguard.SeverityHigh
	}

	return riskguard.RuleResult{
		Rule:      r.Name(),
		Triggered: true,
		Score:     score,
		Severity:  severity,
		Reason: fmt.Sprintf("%d transactions for entity %s within %s (limit %d)",
			count, tx.EntityID, r.Window, r.MaxCount),
	}, nil
}
