package rules_test

import (
	"context"
	"testing"

	"github.com/peymanahmadi/riskguard/internal/store/memory"
	"github.com/peymanahmadi/riskguard/pkg/riskguard"
	"github.com/peymanahmadi/riskguard/pkg/riskguard/rules"
)

func TestBlacklistRule(t *testing.T) {
	bl := memory.NewBlacklist()
	bl.Add("ip", "1.2.3.4")
	rule := rules.NewBlacklistRule(bl)

	clean := riskguard.Transaction{IP: "5.6.7.8", DeviceID: "d1", EntityID: "alice"}
	res, err := rule.Evaluate(context.Background(), clean)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Triggered {
		t.Fatal("clean transaction should not trigger blacklist rule")
	}

	dirty := riskguard.Transaction{IP: "1.2.3.4", DeviceID: "d1", EntityID: "alice"}
	res, err = rule.Evaluate(context.Background(), dirty)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Triggered || res.Score != 100 {
		t.Fatalf("expected max-score trigger for blacklisted ip, got %+v", res)
	}
}

func TestNewDeviceRule(t *testing.T) {
	profiles := memory.NewProfileStore()
	rule := rules.NewNewDeviceRule(profiles)

	// First-ever transaction: no known devices yet, should not flag.
	tx := riskguard.Transaction{EntityID: "alice", DeviceID: "phone-1"}
	res, err := rule.Evaluate(context.Background(), tx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Triggered {
		t.Fatal("first transaction ever should not flag as new device")
	}

	// Establish known device.
	_ = profiles.SaveProfile(context.Background(), riskguard.Profile{
		EntityID:       "alice",
		KnownDeviceIDs: []string{"phone-1"},
	})

	// Same device again: fine.
	res, err = rule.Evaluate(context.Background(), tx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Triggered {
		t.Fatal("known device should not trigger")
	}

	// New device: should flag.
	tx2 := riskguard.Transaction{EntityID: "alice", DeviceID: "laptop-1"}
	res, err = rule.Evaluate(context.Background(), tx2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Triggered {
		t.Fatal("expected new device to be flagged once history exists")
	}
}
