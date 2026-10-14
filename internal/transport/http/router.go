// Package http is the transport layer: routing, middleware and JSON encoding.
// It translates HTTP into service calls and nothing else.
package http

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/demispreviotto/gymproject-backend/internal/config"
	"github.com/demispreviotto/gymproject-backend/internal/domain"
	"github.com/demispreviotto/gymproject-backend/internal/service"
)

type Pinger interface {
	Ping(ctx context.Context) error
}

func NewRouter(log *slog.Logger, cfg config.Config, db Pinger, profiles *service.ProfileService) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(metricsMiddleware)
	r.Use(accessLog(log))
	r.Use(cors(cfg.AllowedOrigin))

	// Operational endpoints (unauthenticated, not exposed through the public LB in prod).
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	r.Get("/readyz", readiness(db))
	r.Handle("/metrics", promhttp.Handler())

	r.Route("/v1", func(r chi.Router) {
		r.Use(requireAuth(cfg.SupabaseJWTKey))
		r.Get("/me", meHandler(profiles))
	})
	return r
}

func readiness(db Pinger) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		ctx, cancel := context.WithTimeout(req.Context(), 2*time.Second)
		defer cancel()
		if err := db.Ping(ctx); err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
}

func meHandler(profiles *service.ProfileService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFrom(r.Context())
		p, err := profiles.Me(r.Context(), userID)
		if errors.Is(err, domain.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "profile not found"})
			return
		}
		if err != nil {
			slog.ErrorContext(r.Context(), "load profile", "err", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
			return
		}
		writeJSON(w, http.StatusOK, p)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
