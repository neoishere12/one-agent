package mcp

import (
	"context"
	"testing"

	"one-agent/internal/platforms"
	"one-agent/internal/types"
)

func TestInitializeReturnsCapabilities(t *testing.T) {
	st := newTestStore(t)
	server := newServerForTests(st, map[types.Platform]platforms.Platform{})

	result, err := server.call(context.Background(), "initialize", mustRaw(t, map[string]any{
		"protocolVersion": "2024-11-05",
		"clientInfo":      map[string]any{"name": "test-client", "version": "1.0.0"},
	}))
	if err != nil {
		t.Fatalf("initialize failed: %v", err)
	}
	out := result.(mcpInitializeOutput)
	if out.ProtocolVersion != mcpProtocolVersion {
		t.Fatalf("ProtocolVersion: got %q", out.ProtocolVersion)
	}
	if out.ServerInfo.Name == "" {
		t.Fatal("ServerInfo.Name must not be empty")
	}
}

func TestToolsListIncludesExpectedTools(t *testing.T) {
	st := newTestStore(t)
	server := newServerForTests(st, map[types.Platform]platforms.Platform{})

	result, err := server.call(context.Background(), "tools/list", mustRaw(t, mcpToolsListInput{}))
	if err != nil {
		t.Fatalf("tools/list failed: %v", err)
	}
	out := result.(mcpToolsListOutput)
	if len(out.Tools) < 10 {
		t.Fatalf("expected at least 10 tools, got %d", len(out.Tools))
	}
	if !hasTool(out.Tools, "get_saved_payment_methods") {
		t.Fatal("missing get_saved_payment_methods")
	}
	if !hasTool(out.Tools, "bootstrap_blinkit_web_session") {
		t.Fatal("missing bootstrap_blinkit_web_session")
	}
}

func TestToolsCallDelegatesToNativeTool(t *testing.T) {
	st := newTestStore(t)
	seedSession(t, st, defaultSession(types.PlatformBlinkit))
	server := newServerForTests(st, map[types.Platform]platforms.Platform{})

	result, err := server.call(context.Background(), "tools/call", mustRaw(t, mcpToolsCallInput{
		Name:      "list_sessions",
		Arguments: mustRaw(t, map[string]any{}),
	}))
	if err != nil {
		t.Fatalf("tools/call failed: %v", err)
	}
	out := result.(mcpToolsCallOutput)
	if out.IsError {
		t.Fatalf("expected success, got error output: %+v", out)
	}
	if out.StructuredContent == nil {
		t.Fatal("StructuredContent must not be nil")
	}
}

func TestToolsCallUnknownToolReturnsErrorResult(t *testing.T) {
	st := newTestStore(t)
	server := newServerForTests(st, map[types.Platform]platforms.Platform{})

	result, err := server.call(context.Background(), "tools/call", mustRaw(t, mcpToolsCallInput{
		Name:      "unknown_tool",
		Arguments: mustRaw(t, map[string]any{}),
	}))
	if err != nil {
		t.Fatalf("tools/call should not return rpc error: %v", err)
	}
	out := result.(mcpToolsCallOutput)
	if !out.IsError {
		t.Fatal("expected IsError=true for unknown tool")
	}
}

func TestPingMethodReturnsEmptyObject(t *testing.T) {
	st := newTestStore(t)
	server := newServerForTests(st, map[types.Platform]platforms.Platform{})

	result, err := server.call(context.Background(), "ping", mustRaw(t, map[string]any{}))
	if err != nil {
		t.Fatalf("ping failed: %v", err)
	}
	out := result.(map[string]any)
	if len(out) != 0 {
		t.Fatalf("expected empty object, got %+v", out)
	}
}

func TestInitializedAliasSupported(t *testing.T) {
	st := newTestStore(t)
	server := newServerForTests(st, map[types.Platform]platforms.Platform{})

	result, err := server.call(context.Background(), "initialized", mustRaw(t, map[string]any{}))
	if err != nil {
		t.Fatalf("initialized failed: %v", err)
	}
	out := result.(map[string]any)
	if out["status"] != "ok" {
		t.Fatalf("expected status=ok, got %+v", out)
	}
}

func hasTool(tools []mcpTool, name string) bool {
	for _, tool := range tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}
