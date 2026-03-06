package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewHTTPMuxRoutesLoginPortal(t *testing.T) {
	called := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	})

	mux := newHTTPMux(handler)
	req := httptest.NewRequest(http.MethodGet, "/login/blinkit?login_id=test", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if !called {
		t.Fatal("expected login portal request to reach MCP handler")
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status code: got %d want %d", rec.Code, http.StatusNoContent)
	}
}
