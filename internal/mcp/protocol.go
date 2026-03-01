package mcp

import (
	"context"
	"encoding/json"
)

const (
	mcpProtocolVersion = "2024-11-05"
	mcpServerName      = "one-agent"
	mcpServerVersion   = "0.1.0"
)

type mcpInitializeInput struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
	ClientInfo      mcpPeerInfo    `json:"clientInfo"`
}

type mcpPeerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type mcpInitializeOutput struct {
	ProtocolVersion string          `json:"protocolVersion"`
	Capabilities    mcpCapabilities `json:"capabilities"`
	ServerInfo      mcpPeerInfo     `json:"serverInfo"`
	Instructions    string          `json:"instructions,omitempty"`
}

type mcpCapabilities struct {
	Tools map[string]any `json:"tools"`
}

type mcpToolsListInput struct {
	Cursor string `json:"cursor"`
}

type mcpToolsListOutput struct {
	Tools []mcpTool `json:"tools"`
}

type mcpTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type mcpToolsCallInput struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type mcpToolsCallOutput struct {
	Content           []mcpContent `json:"content,omitempty"`
	StructuredContent any          `json:"structuredContent,omitempty"`
	IsError           bool         `json:"isError,omitempty"`
}

type mcpContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (s *Server) handleMCPInitialize(raw []byte) (any, *toolError) {
	var input mcpInitializeInput
	if err := decodeParams(raw, &input); err != nil {
		return nil, err
	}
	return mcpInitializeOutput{
		ProtocolVersion: mcpProtocolVersion,
		Capabilities:    mcpCapabilities{Tools: map[string]any{}},
		ServerInfo:      mcpPeerInfo{Name: mcpServerName, Version: mcpServerVersion},
		Instructions:    "Use tools/list then tools/call for quick-commerce actions.",
	}, nil
}

func (s *Server) handleMCPToolsList(raw []byte) (any, *toolError) {
	var input mcpToolsListInput
	if err := decodeParams(raw, &input); err != nil {
		return nil, err
	}
	return mcpToolsListOutput{Tools: mcpToolsCatalog()}, nil
}

func (s *Server) handleMCPToolsCall(ctx context.Context, raw []byte) (any, *toolError) {
	var input mcpToolsCallInput
	if err := decodeParams(raw, &input); err != nil {
		return nil, err
	}
	if input.Name == "" {
		return nil, invalidParams("tools/call requires name", nil)
	}
	args := normalizeToolCallArgs(input.Arguments)
	result, err := s.callNativeTool(ctx, input.Name, args)
	if err != nil {
		return mcpToolError(err), nil
	}
	return mcpToolSuccess(result), nil
}

func normalizeToolCallArgs(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" {
		return []byte(`{}`)
	}
	return raw
}

func mcpToolSuccess(result any) mcpToolsCallOutput {
	return mcpToolsCallOutput{
		Content:           []mcpContent{{Type: "text", Text: toJSONText(result)}},
		StructuredContent: result,
	}
}

func mcpToolError(err *toolError) mcpToolsCallOutput {
	return mcpToolsCallOutput{
		IsError: true,
		Content: []mcpContent{{Type: "text", Text: err.Error()}},
	}
}

func toJSONText(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "ok"
	}
	return string(b)
}

func mcpToolsCatalog() []mcpTool {
	return []mcpTool{
		startLoginTool(),
		loginStatusTool(),
		captureSessionTool(),
		bootstrapBlinkitWebSessionTool(),
		blinkitWorkerStatusTool(),
		reverifyBlinkitSessionTool(),
		listSessionsTool(),
		refreshTokensTool(),
		searchProductTool(),
		comparePricesTool(),
		getSavedAddressesTool(),
		getSavedPaymentMethodsTool(),
		placeOrderTool(),
		getOrderStatusTool(),
	}
}

func captureSessionTool() mcpTool {
	return mcpTool{
		Name:        "capture_session",
		Description: "Wait for a new session to be ingested for the selected app.",
		InputSchema: objectSchema(
			map[string]any{"app": enumString("blinkit", "zepto", "instamart")},
			"app",
		),
	}
}

func listSessionsTool() mcpTool {
	return mcpTool{
		Name:        "list_sessions",
		Description: "List captured sessions and token validity state.",
		InputSchema: objectSchema(map[string]any{}, ""),
	}
}

func refreshTokensTool() mcpTool {
	return mcpTool{
		Name:        "refresh_tokens",
		Description: "Refresh tokens for one app or all apps if app is omitted.",
		InputSchema: objectSchema(
			map[string]any{"app": enumString("blinkit", "zepto", "instamart")},
			"",
		),
	}
}

func searchProductTool() mcpTool {
	return mcpTool{
		Name:        "search_product",
		Description: "Search products across selected apps at a location.",
		InputSchema: objectSchema(
			map[string]any{
				"query":     stringSchema(),
				"apps":      arrayOf(enumString("blinkit", "zepto", "instamart")),
				"latitude":  numberSchema(),
				"longitude": numberSchema(),
			},
			"query",
		),
	}
}

func comparePricesTool() mcpTool {
	return mcpTool{
		Name:        "compare_prices",
		Description: "Compare product prices across all quick-commerce apps.",
		InputSchema: objectSchema(
			map[string]any{
				"query":     stringSchema(),
				"latitude":  numberSchema(),
				"longitude": numberSchema(),
			},
			"query",
		),
	}
}

func getSavedAddressesTool() mcpTool {
	return mcpTool{
		Name:        "get_saved_addresses",
		Description: "Return saved delivery addresses for one app session.",
		InputSchema: objectSchema(
			map[string]any{"app": enumString("blinkit", "zepto", "instamart")},
			"app",
		),
	}
}

func getSavedPaymentMethodsTool() mcpTool {
	return mcpTool{
		Name:        "get_saved_payment_methods",
		Description: "Return saved payment methods and extra payment modes (COD, UPI intent).",
		InputSchema: objectSchema(
			map[string]any{"app": enumString("blinkit", "zepto", "instamart")},
			"app",
		),
	}
}

func placeOrderTool() mcpTool {
	return mcpTool{
		Name:        "place_order",
		Description: "Place an order after user confirmation using saved, COD, or UPI intent payment mode.",
		InputSchema: objectSchema(
			map[string]any{
				"app":           enumString("blinkit", "zepto", "instamart"),
				"product_id":    stringSchema(),
				"address_id":    stringSchema(),
				"payment_id":    stringSchema(),
				"payment_token": stringSchema(),
				"payment_mode":  enumString(paymentModeSaved, paymentModeCOD, paymentModeUPIIntent),
				"quantity":      map[string]any{"type": "integer", "minimum": 1},
				"confirm":       map[string]any{"type": "boolean"},
			},
			"app", "product_id", "address_id", "confirm",
		),
	}
}

func getOrderStatusTool() mcpTool {
	return mcpTool{
		Name:        "get_order_status",
		Description: "Get current status and ETA for an existing order.",
		InputSchema: objectSchema(
			map[string]any{
				"app":      enumString("blinkit", "zepto", "instamart"),
				"order_id": stringSchema(),
			},
			"app", "order_id",
		),
	}
}

func objectSchema(properties map[string]any, required ...string) map[string]any {
	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties":           properties,
	}
	if len(required) == 0 || (len(required) == 1 && required[0] == "") {
		return schema
	}
	schema["required"] = required
	return schema
}

func enumString(values ...string) map[string]any {
	return map[string]any{"type": "string", "enum": values}
}

func stringSchema() map[string]any {
	return map[string]any{"type": "string"}
}

func numberSchema() map[string]any {
	return map[string]any{"type": "number"}
}

func arrayOf(items map[string]any) map[string]any {
	return map[string]any{"type": "array", "items": items}
}
