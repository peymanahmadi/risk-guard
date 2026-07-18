package riskguard

import (
	"context"
	"errors"
	"testing"
	"time"
)

func rule(name string, score float64, triggered bool) Rule {
	return RuleFunc{FuncName: name, Fn: func(ctx context.Context, tx Transaction) (RuleResult, error) {
		return RuleResult{Rule: name, Score: score, Triggered: triggered}, nil
	}}
}

func erroringRule(name string) Rule {
	return RuleFunc{FuncName: name, Fn: func(ctx context.Context, tx Transaction) (RuleResult, error) {
		return RuleResult{}, errors.New("boom")
	}}
}

func panickingRule(name string) Rule {
	return RuleFunc{FuncName: name, Fn: func(ctx context.Context, tx Transaction) (RuleResult, error) {
		panic("kaboom")
	}}
}

func TestEngine_Evaluate_Approve(t *testing.T) {
	e := NewEngine(
		WithRules(rule("low", 5, false), rule("also-low", 10, false)),
		WithScorer(WeightedScorer{}),
		WithThresholds(Thresholds{Review: 40, Decline: 75}),
	)

	v, err := e.Evaluate(context.Background(), Transaction{ID: "tx1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.Decision != Approve {
		t.Fatalf("expected Approve, got %v (score %v)", v.Decision, v.Score)
	}
}

func TestEngine_Evaluate_Decline(t *testing.T) {
	e := NewEngine(
		WithRules(rule("blacklist", 100, true)),
		WithScorer(MaxScorer{}),
		WithThresholds(DefaultThresholds),
	)

	v, err := e.Evaluate(context.Background(), Transaction{ID: "tx2"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.Decision != Decline {
		t.Fatalf("expected Decline, got %v (score %v)", v.Decision, v.Score)
	}
	if len(v.Triggered) != 1 || v.Triggered[0].Rule != "blacklist" {
		t.Fatalf("expected blacklist rule in Triggered, got %+v", v.Triggered)
	}
}

func TestEngine_Evaluate_ResultOrderIsStable(t *testing.T) {
	e := NewEngine(WithRules(
		rule("a", 1, false),
		rule("b", 2, false),
		rule("c", 3, false),
	))

	v, err := e.Evaluate(context.Background(), Transaction{ID: "tx3"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"a", "b", "c"}
	for i, r := range v.Results {
		if r.Rule != want[i] {
			t.Fatalf("results out of order: got %v, want %v", ruleNames(v.Results), want)
		}
	}
}

func ruleNames(results []RuleResult) []string {
	out := make([]string, len(results))
	for i, r := range results {
		out[i] = r.Rule
	}
	return out
}

func TestEngine_Evaluate_FailOpen_IgnoresErroringRule(t *testing.T) {
	e := NewEngine(
		WithRules(rule("ok", 0, false), erroringRule("broken")),
		WithFailurePolicy(FailOpen),
	)

	v, err := e.Evaluate(context.Background(), Transaction{ID: "tx4"})
	if err == nil {
		t.Fatal("expected error to be surfaced even in FailOpen mode")
	}
	if v.Decision != Approve {
		t.Fatalf("expected Approve despite broken rule, got %v", v.Decision)
	}
	if len(v.Results) != 1 {
		t.Fatalf("expected only the succeeding rule's result, got %d", len(v.Results))
	}
}

func TestEngine_Evaluate_FailClosed_ForcesReview(t *testing.T) {
	e := NewEngine(
		WithRules(rule("ok", 0, false), erroringRule("broken")),
		WithFailurePolicy(FailClosed),
		WithThresholds(Thresholds{Review: 40, Decline: 75}),
	)

	v, err := e.Evaluate(context.Background(), Transaction{ID: "tx5"})
	if err == nil {
		t.Fatal("expected error")
	}
	if v.Decision != Review {
		t.Fatalf("expected Review under FailClosed, got %v", v.Decision)
	}
}

func TestEngine_Evaluate_RecoversFromPanickingRule(t *testing.T) {
	e := NewEngine(WithRules(rule("ok", 0, false), panickingRule("evil")))

	v, err := e.Evaluate(context.Background(), Transaction{ID: "tx6"})
	if err == nil {
		t.Fatal("expected the panic to surface as an error")
	}
	if v.Decision != Approve {
		t.Fatalf("expected engine to survive the panic and still decide, got %v", v.Decision)
	}
}

func TestEngine_Evaluate_RespectsTimeout(t *testing.T) {
	slow := RuleFunc{FuncName: "slow", Fn: func(ctx context.Context, tx Transaction) (RuleResult, error) {
		select {
		case <-time.After(200 * time.Millisecond):
			return RuleResult{Rule: "slow"}, nil
		case <-ctx.Done():
			return RuleResult{}, ctx.Err()
		}
	}}

	e := NewEngine(WithRules(slow), WithTimeout(10*time.Millisecond))

	start := time.Now()
	_, err := e.Evaluate(context.Background(), Transaction{ID: "tx7"})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected timeout error")
	}
	if elapsed > 100*time.Millisecond {
		t.Fatalf("engine did not respect timeout, took %v", elapsed)
	}
}

func TestNewEngine_PanicsWithNoRules(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected NewEngine to panic with zero rules")
		}
	}()
	NewEngine()
}

func TestWeightedScorer_Aggregate(t *testing.T) {
	s := WeightedScorer{Weights: map[string]float64{"a": 3, "b": 1}}
	results := []RuleResult{
		{Rule: "a", Score: 90},
		{Rule: "b", Score: 10},
	}
	// (90*3 + 10*1) / (3+1) = 280/4 = 70
	got := s.Aggregate(results)
	if got != 70 {
		t.Fatalf("expected 70, got %v", got)
	}
}

func TestMaxScorer_Aggregate(t *testing.T) {
	s := MaxScorer{}
	got := s.Aggregate([]RuleResult{{Score: 12}, {Score: 88}, {Score: 40}})
	if got != 88 {
		t.Fatalf("expected 88, got %v", got)
	}
}

func TestClamp(t *testing.T) {
	cases := map[float64]float64{-10: 0, 0: 0, 50: 50, 100: 100, 150: 100}
	for in, want := range cases {
		if got := clamp(in); got != want {
			t.Fatalf("clamp(%v) = %v, want %v", in, got, want)
		}
	}
}
