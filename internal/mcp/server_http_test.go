package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"one-agent/internal/platforms"
	"one-agent/internal/platforms/blinkit"
	"one-agent/internal/store"
	"one-agent/internal/types"
)

func TestServeHTTPUnknownMethodReturnsRPCErrorWithHTTP200(t *testing.T) {
	st := newTestStore(t)
	server := newServerForTests(st, map[types.Platform]platforms.Platform{})
	body := mustRaw(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "unknown_method",
		"params":  map[string]any{},
	})

	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp rpcResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error == nil {
		t.Fatalf("expected JSON-RPC error response, got %+v", resp)
	}
	if resp.Error.Code != errCodeMethodNotFound {
		t.Fatalf("expected method-not-found code, got %d", resp.Error.Code)
	}
}

func TestServeHTTPBatchRequest(t *testing.T) {
	st := newTestStore(t)
	server := newServerForTests(st, map[types.Platform]platforms.Platform{})
	body := mustRaw(t, []map[string]any{
		{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "initialize",
			"params": map[string]any{
				"protocolVersion": "2024-11-05",
				"clientInfo":      map[string]any{"name": "test", "version": "1.0.0"},
			},
		},
		{
			"jsonrpc": "2.0",
			"method":  "notifications/initialized",
			"params":  map[string]any{},
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var responses []rpcResponse
	if err := json.Unmarshal(w.Body.Bytes(), &responses); err != nil {
		t.Fatalf("decode batch response: %v", err)
	}
	if len(responses) != 1 {
		t.Fatalf("expected one batch response, got %d", len(responses))
	}
	if responses[0].Error != nil {
		t.Fatalf("unexpected error in batch response: %+v", responses[0].Error)
	}
}

func TestServeHTTPGetMCPReturnsProbePayload(t *testing.T) {
	st := newTestStore(t)
	server := newServerForTests(st, map[types.Platform]platforms.Platform{})

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload["status"] != "ok" {
		t.Fatalf("expected status=ok, got %+v", payload)
	}
}

func TestServeHTTPGetMCPWithEventStreamAcceptReturnsProbePayload(t *testing.T) {
	st := newTestStore(t)
	server := newServerForTests(st, map[types.Platform]platforms.Platform{})

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req.Header.Set("Accept", "text/event-stream")
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestServeHTTPOptionsReturnsNoContent(t *testing.T) {
	st := newTestStore(t)
	server := newServerForTests(st, map[types.Platform]platforms.Platform{})

	req := httptest.NewRequest(http.MethodOptions, "/mcp", nil)
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
	if w.Header().Get("Access-Control-Allow-Origin") == "" {
		t.Fatal("expected CORS headers to be set")
	}
}

func TestServeHTTPNotificationOnlyReturnsAccepted(t *testing.T) {
	st := newTestStore(t)
	server := newServerForTests(st, map[types.Platform]platforms.Platform{})
	body := mustRaw(t, map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
		"params":  map[string]any{},
	})

	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", w.Code)
	}
}

func TestServeHTTPMessagesRouteSupportsRPC(t *testing.T) {
	st := newTestStore(t)
	server := newServerForTests(st, map[types.Platform]platforms.Platform{})
	body := mustRaw(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "ping",
		"params":  map[string]any{},
	})

	req := httptest.NewRequest(http.MethodPost, "/messages", bytes.NewReader(body))
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp rpcResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected rpc error: %+v", resp.Error)
	}
}

func TestServeHTTPSSEPostIsMethodNotAllowed(t *testing.T) {
	st := newTestStore(t)
	server := newServerForTests(st, map[types.Platform]platforms.Platform{})

	req := httptest.NewRequest(http.MethodPost, "/sse", bytes.NewReader([]byte(`{}`)))
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d: %s", w.Code, w.Body.String())
	}
}

func TestServeHTTPLoginPortalRendersPendingFlow(t *testing.T) {
	t.Setenv("BLINKIT_BROWSER_WORKER_URL", "http://worker.local")
	t.Setenv("MCP_PUBLIC_BASE_URL", "https://agent.example.com")
	t.Setenv("BLINKIT_EXTERNAL_LOGIN_URL_TEMPLATE", "https://login.example.com/vnc.html?login_id={login_id_escaped}")
	stubBlinkitWorkerStatus(t, func(ctx context.Context) (*blinkit.BrowserWorkerStatus, error) {
		return &blinkit.BrowserWorkerStatus{
			OK:                 true,
			PageURL:            "about:blank",
			AccessTokenPresent: false,
			AuthKeyPresent:     false,
		}, nil
	})

	release := make(chan struct{})
	stubBlinkitBootstrap(t, func(ctx context.Context, st *store.Store) (*types.AppSession, error) {
		<-release
		return fixtureBlinkitSession(time.Now()), nil
	})

	st := newTestStore(t)
	server := newServerForTests(st, map[types.Platform]platforms.Platform{})
	result, err := server.call(context.Background(), "start_login", mustRaw(t, map[string]any{"app": "blinkit"}))
	if err != nil {
		t.Fatalf("start_login failed: %v", err)
	}
	start := result.(loginStatusOutput)

	req := httptest.NewRequest(http.MethodGet, "/login/blinkit?login_id="+start.LoginID, nil)
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Open Remote Browser") {
		t.Fatalf("expected remote browser button, got %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), start.LoginID) {
		t.Fatalf("expected login id in portal page, got %s", w.Body.String())
	}
	close(release)
}
