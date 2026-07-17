package riskguard

// Decision is the final action recommended for a transaction.
type Decision int

const (
	Approve Decision = iota
	Review
	Decline
)

func (d Decision) String() string {
	switch d {
	case Approve:
		return "approve"
	case Review:
		return "review"
	case Decline:
		return "decline"
	default:
		return "unknown"
	}
}

// Thresholds configures the score cutoffs that map an aggregated risk score
// (0-100) onto a Decision. Scores below Review are approved, scores in
// [Review, Decline) are sent for manual review, and scores >= Decline are
// declined outright.
type Thresholds struct {
	Review  float64
	Decline float64
}

// DefaultThresholds is a conservative starting point; tune per business.
var DefaultThresholds = Thresholds{Review: 40, Decline: 75}

func (t Thresholds) decide(score float64) Decision {
	switch {
	case score >= t.Decline:
		return Decline
	case score >= t.Review:
		return Review
	default:
		return Approve
	}
}

// Verdict is the full result of evaluating a transaction: the aggregated
// score, the resulting decision, and the individual rule results that fed
// into it (for auditability/explainability).
type Verdict struct {
	TransactionID string
	Score         float64
	Decision      Decision
	Results       []RuleResult
	// Triggered is a convenience slice containing only the RuleResults where
	// Triggered == true, in evaluation order.
	Triggered []RuleResult
}

// Reasons returns a human-readable list of reasons the triggered rules
// fired, useful for logging/audit trails and manual review queues.
func (v Verdict) Reasons() []string {
	reasons := make([]string, 0, len(v.Triggered))
	for _, r := range v.Triggered {
		if r.Reason != "" {
			reasons = append(reasons, r.Reason)
		}
	}
	return reasons
}
