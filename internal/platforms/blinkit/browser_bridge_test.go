package blinkit

import (
	"encoding/json"
	"testing"

	"one-agent/internal/types"
)

func TestBrowserModeEnabled(t *testing.T) {
	t.Setenv(envBlinkitSearchMode, "browser")
	if !browserModeEnabled() {
		t.Fatal("expected browser mode enabled")
	}
}

func TestBrowserWorkerURLTrim(t *testing.T) {
	t.Setenv(envBlinkitBrowserWorker, "  http://127.0.0.1:42199/  ")
	if got := browserWorkerURL(); got != "http://127.0.0.1:42199/" {
		t.Fatalf("browserWorkerURL mismatch: %q", got)
	}
}

func TestDecodeBrowserProductsSearchShape(t *testing.T) {
	raw := []byte(`{"products":[{"id":"p1","name":"Amul Lassi","brand":"Amul","price":28,"mrp":30,"unit":"200ml","store_id":"s1","in_stock":true}]}`)
	products, err := decodeBrowserProducts(raw)
	if err != nil {
		t.Fatalf("decodeBrowserProducts: %v", err)
	}
	if len(products) != 1 {
		t.Fatalf("expected 1 product, got %d", len(products))
	}
	if products[0].Platform != types.PlatformBlinkit {
		t.Fatalf("platform mismatch: %q", products[0].Platform)
	}
}

func TestDecodeBrowserBridgeOutput(t *testing.T) {
	raw := json.RawMessage(`{"products":[]}`)
	input := []byte(`{"ok":true,"raw":{"products":[]}}`)
	out, err := decodeBrowserBridgeOutput(input)
	if err != nil {
		t.Fatalf("decodeBrowserBridgeOutput: %v", err)
	}
	if !out.OK {
		t.Fatal("expected ok=true")
	}
	if string(out.Raw) != string(raw) {
		t.Fatalf("raw mismatch: %s", string(out.Raw))
	}
}

func TestDecodeBrowserBridgeOutputErrorPayload(t *testing.T) {
	input := []byte(`{"ok":false,"error":"login required"}`)
	out, err := decodeBrowserBridgeOutput(input)
	if err != nil {
		t.Fatalf("decodeBrowserBridgeOutput: %v", err)
	}
	if out.OK {
		t.Fatal("expected ok=false")
	}
	if out.Error != "login required" {
		t.Fatalf("error mismatch: %q", out.Error)
	}
}
