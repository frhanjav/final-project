package main

import (
	"encoding/json"
	"log"
	"net/http"
)

type InvokeRequest struct {
	RequestID  string          `json:"request_id"`
	DurationMS int             `json:"duration_ms,omitempty"`
	ForceTier  *int            `json:"force_tier,omitempty"`
	Payload    json.RawMessage `json:"payload,omitempty"`
}

type InvokeResponse struct {
	RequestID                string  `json:"request_id"`
	TierUsed                 int     `json:"tier_used"`
	LatencyMS                float64 `json:"latency_ms"`
	ActiveContainersPoolSize int     `json:"active_containers_pool_size"`
	Message                  string  `json:"message"`
}

func NewGateway(manager *PoolManager) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/invoke", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "only POST is supported", http.StatusMethodNotAllowed)
			return
		}

		var req InvokeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON payload", http.StatusBadRequest)
			return
		}

		result, err := manager.Invoke(r.Context(), req)
		if err != nil {
			log.Printf("invoke failed: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(InvokeResponse{
			RequestID:                result.RequestID,
			TierUsed:                 result.TierUsed,
			LatencyMS:                result.LatencyMS,
			ActiveContainersPoolSize: result.ActiveContainersPoolSize,
			Message:                  "invocation completed",
		}); err != nil {
			log.Printf("encode response: %v", err)
		}
	})

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	return mux
}
