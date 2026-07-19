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
	"strings"
	"syscall"
	"time"

	"github.com/peymanahmadi/riskguard/internal/api"
	ikafka "github.com/peymanahmadi/riskguard/internal/kafka"
	"github.com/peymanahmadi/riskguard/internal/store/memory"
	"github.com/peymanahmadi/riskguard/internal/store/postgres"
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
		historyStore riskguard.HistoryStore
		counterStore riskguard.CounterStore
		blacklist    riskguard.Blacklist
	)

	switch cfg.StoreKind {
	case "postgres":
		pg, err := postgres.New(ctx, cfg.DatabaseURL)
		if err != nil {
			return err
		}
		defer pg.Close()
		if err := pg.Migrate(ctx); err != nil {
			return err
		}
		profileStore, historyStore, counterStore, blacklist = pg, pg, pg, pg
		logger.Info("using postgres store")
	default:
		profileStore = memory.NewProfileStore()
		historyStore = memory.NewHistoryStore()
		counterStore = memory.NewCounterStore()
		blacklist = memory.NewBlacklist()
		logger.Info("using in-memory store (set STORE=postgres for a persistent backend)")
	}

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

	if len(cfg.KafkaBrokers) > 0 {
		pipeline := ikafka.NewPipeline(ikafka.Config{
			Brokers:       cfg.KafkaBrokers,
			InputTopic:    cfg.KafkaInputTopic,
			OutputTopic:   cfg.KafkaOutputTopic,
			ConsumerGroup: cfg.KafkaGroup,
		}, engine, historyStore, logger)

		go func() {
			logger.Info("kafka pipeline starting", "brokers", cfg.KafkaBrokers)
			if err := pipeline.Run(ctx); err != nil {
				errCh <- err
			}
		}()
	}

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
	DatabaseURL             string
	AmountThresholdMinor    int64
	AmountThresholdCurrency string
	KafkaBrokers            []string
	KafkaInputTopic         string
	KafkaOutputTopic        string
	KafkaGroup              string
}

func loadConfig() config {
	cfg := config{
		Port:                    getenv("PORT", "8080"),
		StoreKind:               getenv("STORE", "memory"),
		DatabaseURL:             getenv("DATABASE_URL", "postgres://riskguard:riskguard@localhost:5432/riskguard?sslmode=disable"),
		AmountThresholdMinor:    50000, // $500.00
		AmountThresholdCurrency: getenv("AMOUNT_THRESHOLD_CURRENCY", "USD"),
		KafkaInputTopic:         getenv("KAFKA_INPUT_TOPIC", "transactions"),
		KafkaOutputTopic:        getenv("KAFKA_OUTPUT_TOPIC", "risk-decisions"),
		KafkaGroup:              getenv("KAFKA_GROUP", "riskguard"),
	}
	if brokers := os.Getenv("KAFKA_BROKERS"); brokers != "" {
		cfg.KafkaBrokers = strings.Split(brokers, ",")
	}
	return cfg
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
