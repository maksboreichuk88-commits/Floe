package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/floe-dev/floe/internal/config"
	"github.com/floe-dev/floe/internal/provider"
)

// Middleware is an HTTP middleware function.
type Middleware func(http.Handler) http.Handler

// NewMiddlewareChain composes multiple middleware into a single handler.
func NewMiddlewareChain(h http.Handler, mws ...Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

// RequestSizeLimiter rejects requests exceeding maxBytes.
func RequestSizeLimiter(maxBytes int64) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > maxBytes {
				writeJSON(w, http.StatusRequestEntityTooLarge, ErrorResponse{
					Error: fmt.Sprintf("request body too large: %d bytes exceeds limit of %d", r.ContentLength, maxBytes),
				})
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			next.ServeHTTP(w, r)
		})
	}
}

// AuthMiddleware validates bearer tokens when configured.
func AuthMiddleware(token string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if token == "" {
				next.ServeHTTP(w, r)
				return
			}

			auth := r.Header.Get("Authorization")
			if auth == "" {
				writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "missing Authorization header"})
				return
			}

			if !strings.HasPrefix(auth, "Bearer ") {
				writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "invalid Authorization format; expected 'Bearer <token>'"})
				return
			}

			if strings.TrimPrefix(auth, "Bearer ") != token {
				writeJSON(w, http.StatusForbidden, ErrorResponse{Error: "invalid auth token"})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequestLogger logs each incoming request and its duration.
func RequestLogger(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w, status: 200}

			next.ServeHTTP(sw, r)

			logger.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", sw.status,
				"duration_ms", time.Since(start).Milliseconds(),
				"remote", r.RemoteAddr,
			)
		})
	}
}

// CORSMiddleware adds CORS headers for the dashboard.
func CORSMiddleware(allowOrigin string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", allowOrigin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RecoveryMiddleware catches panics and returns a 500 response.
func RecoveryMiddleware(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					logger.Error("panic recovered", "error", err, "path", r.URL.Path)
					writeJSON(w, http.StatusInternalServerError, ErrorResponse{
						Error: "internal server error",
					})
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// ---- HTTP Handler (Gateway API) ----

// GatewayHandler is the main HTTP handler that accepts OpenAI-compatible
// chat completion requests and routes them through the provider pipeline.
type GatewayHandler struct {
	router *Router
	logger *slog.Logger
	cfg    config.ServerConfig
}

// NewGatewayHandler creates a new gateway HTTP handler.
func NewGatewayHandler(router *Router, logger *slog.Logger, cfg config.ServerConfig) *GatewayHandler {
	return &GatewayHandler{
		router: router,
		logger: logger,
		cfg:    cfg,
	}
}

// ServeHTTP handles the /v1/chat/completions endpoint.
func (h *GatewayHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "only POST is allowed"})
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "failed to read request body"})
		return
	}

	var req provider.ChatRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: fmt.Sprintf("invalid JSON: %v", err)})
		return
	}

	req.Metadata = provider.RequestMeta{
		RequestID: generateRequestID(),
		StartTime: time.Now(),
	}

	// Extract project ID from header if present
	if pid := r.Header.Get("X-Floe-Project"); pid != "" {
		req.Metadata.ProjectID = pid
	}

	ctx := r.Context()

	if req.Stream {
		h.handleStream(ctx, w, &req)
		return
	}

	resp, err := h.router.Route(ctx, &req)
	if err != nil {
		status := http.StatusBadGateway
		if ctx.Err() != nil {
			status = http.StatusGatewayTimeout
		}
		writeJSON(w, status, ErrorResponse{Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleStream handles Server-Sent Events streaming responses.
func (h *GatewayHandler) handleStream(ctx context.Context, w http.ResponseWriter, req *provider.ChatRequest) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "streaming not supported"})
		return
	}

	ch, err := h.router.StreamRoute(ctx, req)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, ErrorResponse{Error: err.Error()})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	for chunk := range ch {
		if chunk.Err != nil {
			data, _ := json.Marshal(ErrorResponse{Error: chunk.Err.Error()})
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
			return
		}

		data, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()

		if chunk.Done {
			fmt.Fprintf(w, "data: [DONE]\n\n")
			flusher.Flush()
			return
		}
	}
}

// ---- Helpers ----

// ErrorResponse is the standard error response format.
type ErrorResponse struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func generateRequestID() string {
	return fmt.Sprintf("floe-%d", time.Now().UnixNano())
}
