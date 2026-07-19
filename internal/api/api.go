// Package api exposes the riskguard engine over a small JSON HTTP API for
// the demo service in cmd/server. It intentionally has no framework
// dependency: Go 1.22's stdlib http.ServeMux is expressive enough for a
// handful of routes.
package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/peymanahmadi/riskguard/pkg/riskguard"
)

type Server struct {
	engine *riskguard.Engine
	logger *slog.Logger
	mux    *http.ServeMux
}

func NewServer(engine *riskguard.Engine, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	s := &Server{engine: engine, logger: logger, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", s.handleHealth)
	s.mux.HandleFunc("POST /v1/transactions/evaluate", s.handleEvaluate)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// transactionRequest mirrors riskguard.Transaction but with a JSON-friendly
// shape decoupled from the domain type, so the wire format can evolve
// independently of the library's Go API.
type transactionRequest struct {
	ID            string            `json:"id"`
	EntityID      string            `json:"entity_id"`
	MerchantID    string            `json:"merchant_id"`
	AmountMinor   int64             `json:"amount_minor"`
	Currency      string            `json:"currency"`
	IP            string            `json:"ip"`
	DeviceID      string            `json:"device_id"`
	Country       string            `json:"country"`
	Lat           float64           `json:"lat"`
	Lon           float64           `json:"lon"`
	PaymentMethod string            `json:"payment_method"`
	Metadata      map[string]string `json:"metadata,omitempty"`
}

type verdictResponse struct {
	TransactionID string   `json:"transaction_id"`
	Score         float64  `json:"score"`
	Decision      string   `json:"decision"`
	Reasons       []string `json:"reasons,omitempty"`
}

func (s *Server) handleEvaluate(w http.ResponseWriter, r *http.Request) {
	var req transactionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if req.EntityID == "" {
		http.Error(w, `{"error":"entity_id is required"}`, http.StatusBadRequest)
		return
	}
	if req.ID == "" {
		req.ID = randomID()
	}

	tx := riskguard.Transaction{
		ID:            req.ID,
		EntityID:      req.EntityID,
		MerchantID:    req.MerchantID,
		AmountMinor:   req.AmountMinor,
		Currency:      req.Currency,
		IP:            req.IP,
		DeviceID:      req.DeviceID,
		Country:       req.Country,
		Lat:           req.Lat,
		Lon:           req.Lon,
		PaymentMethod: req.PaymentMethod,
		CreatedAt:     time.Now().UTC(),
		Metadata:      req.Metadata,
	}

	verdict, err := s.engine.Evaluate(r.Context(), tx)
	if err != nil {
		// Degraded-but-usable: log the rule failure(s) but still return the
		// verdict computed from the rules that did succeed.
		s.logger.Warn("evaluation reported rule errors", "tx_id", tx.ID, "error", err)
	}

	resp := verdictResponse{
		TransactionID: verdict.TransactionID,
		Score:         verdict.Score,
		Decision:      verdict.Decision.String(),
		Reasons:       verdict.Reasons(),
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func randomID() string {
	return time.Now().UTC().Format("20060102T150405.000000000")
}
