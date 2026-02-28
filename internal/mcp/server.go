package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"one-agent/internal/platforms"
	"one-agent/internal/refresh"
	"one-agent/internal/store"
	"one-agent/internal/types"
)

// Server is a JSON-RPC HTTP handler exposing MCP tools.
type Server struct {
	st            *store.Store
	platforms     map[types.Platform]platforms.Platform
	ingestSecret  string
	ingestLimiter *ingestRateLimiter
	now           func() time.Time
	searchLimit   time.Duration
	hub           *sseHub
}

// Option customizes Server behavior.
type Option func(*Server)

// WithIngestSecret sets the shared secret validated on POST /sessions/ingest.
func WithIngestSecret(secret string) Option {
	return func(s *Server) { s.ingestSecret = secret }
}

// WithIngestRateLimit sets the per-client /sessions/ingest rate limit.
// Pass max<=0 or window<=0 to disable rate limiting.
func WithIngestRateLimit(max int, window time.Duration) Option {
	return func(s *Server) { s.ingestLimiter = newIngestRateLimiter(max, window) }
}

func withNow(now func() time.Time) Option {
	return func(s *Server) { s.now = now }
}

// New builds an MCP server handler.
func New(st *store.Store, clients map[types.Platform]platforms.Platform, opts ...Option) *Server {
	s := &Server{
		st:            st,
		platforms:     cloneClients(clients),
		ingestLimiter: newIngestRateLimiter(10, time.Minute),
		now:           time.Now,
		searchLimit:   60 * time.Second,
		hub:           newSSEHub(),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// ServeHTTP handles /health, /sessions/ingest, /rpc, /mcp and /sse transports.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.handleHealth(w, r) {
		return
	}
	if s.handleIngest(w, r) {
		return
	}
	switch r.URL.Path {
	case "/sse":
		s.handleSSEEndpoint(w, r)
	case "/mcp":
		s.handleMCPTransport(w, r)
	case "/rpc":
		s.handleRPCTransport(w, r)
	case "/messages", "/message":
		s.handleMessagesTransport(w, r)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}

func (s *Server) handleSSEEndpoint(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		writeNoContent(w)
		return
	}
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	s.handleSSE(w, r)
}

func (s *Server) handleMCPTransport(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		writeNoContent(w)
		return
	}
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "ok",
			"transport": map[string]any{
				"type": "jsonrpc-http",
			},
		})
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	s.handleMCPPost(w, r)
}

func (s *Server) handleRPCTransport(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		writeNoContent(w)
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	s.handleRPCPost(w, r)
}

func (s *Server) handleMessagesTransport(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		writeNoContent(w)
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	s.handleMCPPost(w, r)
}

func (s *Server) handleRPCPost(w http.ResponseWriter, r *http.Request) {
	reqs, isBatch, reqErr := decodeRPCRequests(r.Body)
	if reqErr != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse(nil, reqErr))
		return
	}
	responses := s.executeRPCRequests(r.Context(), reqs)
	writeRPCResponses(w, responses, isBatch)
}

func (s *Server) executeRPCRequests(ctx context.Context, reqs []rpcRequest) []rpcResponse {
	responses := make([]rpcResponse, 0, len(reqs))
	for _, req := range reqs {
		result, callErr := s.call(ctx, req.Method, req.Params)
		if callErr != nil {
			slog.Warn("mcp tool failed", "tool", req.Method, "err", callErr.Error())
			if len(req.ID) > 0 {
				responses = append(responses, errorResponse(req.ID, callErr))
			}
			continue
		}
		if len(req.ID) == 0 {
			continue
		}
		responses = append(responses, successResponse(req.ID, result))
	}
	return responses
}

func writeRPCResponses(w http.ResponseWriter, responses []rpcResponse, isBatch bool) {
	if len(responses) == 0 {
		writeAccepted(w)
		return
	}
	if isBatch {
		writeJSON(w, http.StatusOK, responses)
		return
	}
	writeJSON(w, http.StatusOK, responses[0])
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/health" {
		return false
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	return true
}

func (s *Server) call(ctx context.Context, method string, params json.RawMessage) (any, *toolError) {
	switch method {
	case "initialize":
		return s.handleMCPInitialize(params)
	case "initialized", "notifications/initialized":
		return map[string]any{"status": "ok"}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return s.handleMCPToolsList(params)
	case "tools/call":
		return s.handleMCPToolsCall(ctx, params)
	default:
		return s.callNativeTool(ctx, method, params)
	}
}

func (s *Server) callNativeTool(ctx context.Context, method string, params json.RawMessage) (any, *toolError) {
	switch method {
	case "capture_session":
		return s.handleCaptureSession(ctx, params)
	case "bootstrap_blinkit_web_session":
		return s.handleBootstrapBlinkitWebSession(ctx, params)
	case "list_sessions":
		return s.handleListSessions(ctx)
	case "refresh_tokens":
		return s.handleRefreshTokens(ctx, params)
	case "search_product":
		return s.handleSearchProduct(ctx, params)
	case "compare_prices":
		return s.handleComparePrices(ctx, params)
	case "get_saved_addresses":
		return s.handleGetSavedAddresses(ctx, params)
	case "get_saved_payment_methods":
		return s.handleGetSavedPaymentMethods(ctx, params)
	case "place_order":
		return s.handlePlaceOrder(ctx, params)
	case "get_order_status":
		return s.handleGetOrderStatus(ctx, params)
	default:
		return nil, methodNotFound(method)
	}
}

func decodeParams(raw json.RawMessage, out any) *toolError {
	if len(raw) == 0 || string(raw) == "null" {
		raw = []byte(`{}`)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return invalidParams("invalid params", err)
	}
	return nil
}

func parsePlatform(raw string) (types.Platform, *toolError) {
	app := types.Platform(raw)
	switch app {
	case types.PlatformBlinkit, types.PlatformZepto, types.PlatformInstamart:
		return app, nil
	default:
		return "", invalidParams("invalid app", errors.New(raw))
	}
}

func (s *Server) platform(app types.Platform) (platforms.Platform, *toolError) {
	client, ok := s.platforms[app]
	if !ok {
		return nil, invalidParams("platform client unavailable", errors.New(string(app)))
	}
	return client, nil
}

func (s *Server) refreshOne(ctx context.Context, app types.Platform, client platforms.Platform) (refresh.Result, *toolError) {
	result, err := refresh.RefreshOne(ctx, s.st, app, client)
	if err != nil {
		return result, internalError("refresh failed", err)
	}
	return result, nil
}

func cloneClients(in map[types.Platform]platforms.Platform) map[types.Platform]platforms.Platform {
	out := make(map[types.Platform]platforms.Platform, len(in))
	for app, client := range in {
		out[app] = client
	}
	return out
}
