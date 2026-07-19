// Command server is a small demo application showing riskguard wired up
// end-to-end: a Postgres- or memory-backed engine exposed over HTTP, plus an
// optional Kafka consumer/producer pipeline for asynchronous evaluation.
//
// It exists to demonstrate integration, not as a production-ready service —
// see README.md for what a real deployment would add (auth, rate limiting,
// structured audit logging, metrics, etc).
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/peymanahmadi/riskguard/internal/api"
	"github.com/peymanahmadi/riskguard/internal/store/memory"
	"github.com/peymanahmadi/riskguard/pkg/riskguard"
	"github.com/peymanahmadi/riskguard/pkg/riskguard/rules"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	if err := run(logger); err != nil {
		logger.Error("server exited with error", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg := loadConfig()

	var (
		profileStore riskguard.ProfileStore
		//historyStore riskguard.HistoryStore
		counterStore riskguard.CounterStore
		blacklist    riskguard.Blacklist
	)

	profileStore = memory.NewProfileStore()
	//historyStore = memory.NewHistoryStore()
	counterStore = memory.NewCounterStore()
	blacklist = memory.NewBlacklist()
	logger.Info("using in-memory store (set STORE=postgres for a persistent backend)")

	engine := riskguard.NewEngine(
		riskguard.WithRules(
			rules.NewVelocityRule(counterStore, 5*time.Minute, 10),
			rules.NewAmountThresholdRule(cfg.AmountThresholdMinor, cfg.AmountThresholdCurrency),
			rules.NewGeoVelocityRule(profileStore),
			rules.NewNewDeviceRule(profileStore),
			rules.NewBlacklistRule(blacklist),
		),
		riskguard.WithScorer(riskguard.WeightedScorer{
			Weights: map[string]float64{"blacklist": 4, "geo_velocity": 3},
		}),
		riskguard.WithThresholds(riskguard.DefaultThresholds),
		riskguard.WithTimeout(2*time.Second),
		riskguard.WithFailurePolicy(riskguard.FailOpen),
	)

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      api.NewServer(engine, logger),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 2)

	go func() {
		logger.Info("http server listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	case err := <-errCh:
		return err
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

type config struct {
	Port                    string
	StoreKind               string
	AmountThresholdMinor    int64
	AmountThresholdCurrency string
}

func loadConfig() config {
	cfg := config{
		Port:                    getenv("PORT", "8080"),
		StoreKind:               getenv("STORE", "memory"),
		AmountThresholdMinor:    50000, // $500.00
		AmountThresholdCurrency: getenv("AMOUNT_THRESHOLD_CURRENCY", "USD"),
	}
	return cfg
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
