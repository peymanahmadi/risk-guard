package rules_test

import (
	"context"
	"testing"
	"time"

	"github.com/peymanahmadi/riskguard/internal/store/memory"
	"github.com/peymanahmadi/riskguard/pkg/riskguard"
	"github.com/peymanahmadi/riskguard/pkg/riskguard/rules"
)

func TestVelocityRule_TriggersAfterLimit(t *testing.T) {
	store := memory.NewCounterStore()
	rule := rules.NewVelocityRule(store, time.Minute, 3)

	tx := riskguard.Transaction{EntityID: "alice"}

	for i := 0; i < 3; i++ {
		res, err := rule.Evaluate(context.Background(), tx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Triggered {
			t.Fatalf("call %d: rule triggered before exceeding limit", i+1)
		}
	}

	res, err := rule.Evaluate(context.Background(), tx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Triggered {
		t.Fatal("expected rule to trigger on the 4th transaction (limit 3)")
	}
	if res.Score <= 0 {
		t.Fatalf("expected positive score, got %v", res.Score)
	}
}

func TestVelocityRule_SeparatesEntities(t *testing.T) {
	store := memory.NewCounterStore()
	rule := rules.NewVelocityRule(store, time.Minute, 1)

	_, _ = rule.Evaluate(context.Background(), riskguard.Transaction{EntityID: "alice"})
	res, _ := rule.Evaluate(context.Background(), riskguard.Transaction{EntityID: "bob"})

	if res.Triggered {
		t.Fatal("bob's first transaction should not trigger due to alice's activity")
	}
}
