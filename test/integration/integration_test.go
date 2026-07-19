//go:build integration

// Run with: make up && make integration-test
//
// These tests exercise the Postgres and Kafka backed implementations
// against the real services started by docker-compose.yml, rather than the
// in-memory fakes used by the unit tests elsewhere in the repo.
package integration

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/peymanahmadi/riskguard/internal/kafka"
	"github.com/peymanahmadi/riskguard/internal/store/postgres"
	"github.com/peymanahmadi/riskguard/pkg/riskguard"
)

func mustEnv(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		t.Skipf("%s not set; run `make up` and use `make integration-test`", key)
	}
	return v
}

func TestPostgresStore_ProfileRoundTrip(t *testing.T) {
	dsn := mustEnv(t, "DATABASE_URL")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	store, err := postgres.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	entityID := "integration-test-entity"
	want := riskguard.Profile{
		EntityID:       entityID,
		HomeCountry:    "US",
		LastCountry:    "US",
		LastLat:        37.7749,
		LastLon:        -122.4194,
		LastSeenAt:     time.Now().UTC().Truncate(time.Second),
		KnownDeviceIDs: []string{"device-a", "device-b"},
	}

	if err := store.SaveProfile(ctx, want); err != nil {
		t.Fatalf("save profile: %v", err)
	}

	got, err := store.GetProfile(ctx, entityID)
	if err != nil {
		t.Fatalf("get profile: %v", err)
	}

	if got.HomeCountry != want.HomeCountry || got.LastCountry != want.LastCountry {
		t.Fatalf("profile mismatch: got %+v, want %+v", got, want)
	}
	if !got.KnowsDevice("device-a") || !got.KnowsDevice("device-b") {
		t.Fatalf("expected both known devices to round-trip, got %+v", got.KnownDeviceIDs)
	}
}

func TestPostgresStore_CounterStore_SlidingWindow(t *testing.T) {
	dsn := mustEnv(t, "DATABASE_URL")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	store, err := postgres.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	key := "integration-test-counter"
	for i := 0; i < 3; i++ {
		if _, err := store.Increment(ctx, key, time.Minute); err != nil {
			t.Fatalf("increment: %v", err)
		}
	}
	count, err := store.Increment(ctx, key, time.Minute)
	if err != nil {
		t.Fatalf("increment: %v", err)
	}
	if count != 4 {
		t.Fatalf("expected count 4, got %d", count)
	}
}

func TestKafkaPipeline_ProduceConsumeRoundTrip(t *testing.T) {
	brokersEnv := mustEnv(t, "KAFKA_BROKERS")
	brokers := []string{brokersEnv}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	event := kafka.TransactionEvent{
		ID:          "integration-tx-1",
		EntityID:    "integration-entity",
		AmountMinor: 1000,
		Currency:    "USD",
		CreatedAt:   time.Now().UTC(),
	}

	if err := kafka.PublishTransaction(ctx, brokers, "transactions", event); err != nil {
		t.Fatalf("publish: %v", err)
	}

	// A full assertion would consume from "risk-decisions" and verify the
	// engine produced a matching DecisionEvent; that requires the demo
	// server (or a Pipeline) to already be running against this broker,
	// which `make up` does. Here we only assert that publishing succeeds,
	// keeping this test independent of server lifecycle timing.
}
