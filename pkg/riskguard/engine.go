package riskguard

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// FailurePolicy decides how the Engine treats a Rule that returns an error
// (as opposed to a normal, non-triggered RuleResult).
type FailurePolicy int

const (
	// FailOpen ignores rules that error out: the transaction is scored using
	// only the rules that succeeded. Prefer this for availability-sensitive
	// paths where a single flaky rule shouldn't block all payments.
	FailOpen FailurePolicy = iota
	// FailClosed treats any rule error as an automatic Review decision,
	// regardless of the score from the rules that did succeed. Prefer this
	// when missing signal is itself a risk (e.g. compliance-mandated checks).
	FailClosed
)

// Engine evaluates a Transaction against a fixed set of Rules concurrently,
// aggregates their results with a Scorer, and maps the resulting score onto
// a Decision using Thresholds.
type Engine struct {
	rules      []Rule
	scorer     Scorer
	thresholds Thresholds
	timeout    time.Duration
	failPolicy FailurePolicy
}

// Option configures an Engine.
type Option func(*Engine)

func WithRules(rules ...Rule) Option {
	return func(e *Engine) { e.rules = append(e.rules, rules...) }
}

func WithScorer(s Scorer) Option {
	return func(e *Engine) { e.scorer = s }
}

func WithThresholds(t Thresholds) Option {
	return func(e *Engine) { e.thresholds = t }
}

// WithTimeout bounds how long a single Evaluate call may take across all
// rules combined. A zero timeout (the default) means no bound is applied
// beyond whatever the caller's context already carries.
func WithTimeout(d time.Duration) Option {
	return func(e *Engine) { e.timeout = d }
}

func WithFailurePolicy(p FailurePolicy) Option {
	return func(e *Engine) { e.failPolicy = p }
}

// NewEngine builds an Engine from the given options. It panics if no rules
// are configured, since an engine with no rules is almost certainly a
// misconfiguration rather than an intentional "approve everything" policy —
// callers who genuinely want that should say so explicitly with a no-op
// rule.
func NewEngine(opts ...Option) *Engine {
	e := &Engine{
		scorer:     WeightedScorer{},
		thresholds: DefaultThresholds,
		failPolicy: FailOpen,
	}
	for _, opt := range opts {
		opt(e)
	}
	if len(e.rules) == 0 {
		panic("riskguard: engine configured with zero rules")
	}
	return e
}

// Evaluate runs every configured rule against tx concurrently and returns
// the aggregated Verdict. Rule order in the returned Verdict.Results matches
// the order rules were registered, regardless of completion order.
func (e *Engine) Evaluate(ctx context.Context, tx Transaction) (Verdict, error) {
	if e.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, e.timeout)
		defer cancel()
	}

	results := make([]RuleResult, len(e.rules))
	errs := make([]error, len(e.rules))

	var wg sync.WaitGroup
	wg.Add(len(e.rules))
	for i, rule := range e.rules {
		i, rule := i, rule
		go func() {
			defer wg.Done()
			res, err := safeEvaluate(ctx, rule, tx)
			if err != nil {
				errs[i] = fmt.Errorf("rule %q: %w", rule.Name(), err)
				return
			}
			results[i] = res
		}()
	}
	wg.Wait()

	var succeeded []RuleResult
	var failed []error
	for i, res := range results {
		if errs[i] != nil {
			failed = append(failed, errs[i])
			continue
		}
		succeeded = append(succeeded, res)
	}

	if len(failed) > 0 && e.failPolicy == FailClosed {
		return Verdict{
			TransactionID: tx.ID,
			Score:         e.thresholds.Review,
			Decision:      Review,
			Results:       succeeded,
			Triggered:     triggeredOnly(succeeded),
		}, joinErrors(failed)
	}

	score := e.scorer.Aggregate(succeeded)
	verdict := Verdict{
		TransactionID: tx.ID,
		Score:         score,
		Decision:      e.thresholds.decide(score),
		Results:       succeeded,
		Triggered:     triggeredOnly(succeeded),
	}

	if len(failed) > 0 {
		// FailOpen: still return a usable verdict, but surface the errors so
		// the caller can log/alert on degraded rule coverage.
		return verdict, joinErrors(failed)
	}
	return verdict, nil
}

// safeEvaluate recovers from a panicking Rule so that one badly-behaved rule
// cannot take down evaluation for every other rule running concurrently.
func safeEvaluate(ctx context.Context, rule Rule, tx Transaction) (res RuleResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return rule.Evaluate(ctx, tx)
}

func triggeredOnly(results []RuleResult) []RuleResult {
	out := make([]RuleResult, 0, len(results))
	for _, r := range results {
		if r.Triggered {
			out = append(out, r)
		}
	}
	return out
}

func joinErrors(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	if len(errs) == 1 {
		return errs[0]
	}
	msg := fmt.Sprintf("%d rule(s) failed: %v", len(errs), errs[0])
	return fmt.Errorf("%s (+%d more)", msg, len(errs)-1)
}
