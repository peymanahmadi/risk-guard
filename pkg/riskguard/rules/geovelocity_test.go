package rules_test

import (
	"context"
	"testing"
	"time"

	"github.com/peymanahmadi/riskguard/internal/store/memory"
	"github.com/peymanahmadi/riskguard/pkg/riskguard"
	"github.com/peymanahmadi/riskguard/pkg/riskguard/rules"
)

func TestGeoVelocityRule_NoPriorHistory(t *testing.T) {
	store := memory.NewProfileStore()
	rule := rules.NewGeoVelocityRule(store)

	tx := riskguard.Transaction{EntityID: "alice", CreatedAt: time.Now(), Lat: 51.5, Lon: -0.1}
	res, err := rule.Evaluate(context.Background(), tx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Triggered {
		t.Fatal("should not trigger with no prior profile")
	}
}

func TestGeoVelocityRule_ImpossibleTravel(t *testing.T) {
	store := memory.NewProfileStore()
	now := time.Now()

	// London
	_ = store.SaveProfile(context.Background(), riskguard.Profile{
		EntityID: "alice", LastCountry: "GB", LastLat: 51.5074, LastLon: -0.1278, LastSeenAt: now,
	})

	rule := rules.NewGeoVelocityRule(store)

	// Tokyo, 3 minutes later — ~9500km, physically impossible in 3 minutes.
	tx := riskguard.Transaction{
		EntityID:  "alice",
		Country:   "JP",
		Lat:       35.6762,
		Lon:       139.6503,
		CreatedAt: now.Add(3 * time.Minute),
	}

	res, err := rule.Evaluate(context.Background(), tx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Triggered {
		t.Fatal("expected impossible travel to be flagged")
	}
}

func TestGeoVelocityRule_PlausibleTravel(t *testing.T) {
	store := memory.NewProfileStore()
	now := time.Now()

	// London
	_ = store.SaveProfile(context.Background(), riskguard.Profile{
		EntityID: "alice", LastCountry: "GB", LastLat: 51.5074, LastLon: -0.1278, LastSeenAt: now,
	})

	rule := rules.NewGeoVelocityRule(store)

	// Paris, 12 hours later — entirely plausible (~340km).
	tx := riskguard.Transaction{
		EntityID:  "alice",
		Country:   "FR",
		Lat:       48.8566,
		Lon:       2.3522,
		CreatedAt: now.Add(12 * time.Hour),
	}

	res, err := rule.Evaluate(context.Background(), tx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Triggered {
		t.Fatalf("plausible travel incorrectly flagged: %+v", res)
	}
}
