// Package riskguard provides a pluggable, concurrency-safe engine for
// evaluating payment transactions against a set of fraud/abuse detection
// rules and producing a risk decision (approve / review / decline).
//
// The engine itself has zero knowledge of how rules compute risk, how
// history/counters are stored, or how scores are aggregated: everything is
// expressed as small interfaces (Rule, Scorer, CounterStore, ProfileStore,
// HistoryStore) so that callers can plug in Postgres, Redis, in-memory
// stores, or entirely custom rules without touching the engine.
//
// Typical usage:
//
//	engine := riskguard.NewEngine(
//		riskguard.WithRules(
//			rules.NewVelocityRule(counterStore, 5*time.Minute, 10),
//			rules.NewAmountThresholdRule(500_00, "USD"),
//			rules.NewNewDeviceRule(profileStore),
//		),
//		riskguard.WithScorer(riskguard.WeightedScorer{}),
//		riskguard.WithThresholds(riskguard.Thresholds{Review: 40, Decline: 75}),
//	)
//
//	verdict, err := engine.Evaluate(ctx, tx)
package riskguard
