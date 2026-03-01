package mcp

func blinkitWorkerStatusTool() mcpTool {
	return mcpTool{
		Name:        "blinkit_worker_status",
		Description: "Inspect Blinkit browser worker session/challenge state for fast diagnostics.",
		InputSchema: objectSchema(
			map[string]any{
				"timeout_seconds": map[string]any{"type": "integer", "minimum": 1, "maximum": 30},
			},
			"",
		),
	}
}

func reverifyBlinkitSessionTool() mcpTool {
	return mcpTool{
		Name:        "reverify_blinkit_session",
		Description: "Run Blinkit browser re-verification/bootstrap and persist session when verification completes.",
		InputSchema: objectSchema(
			map[string]any{
				"timeout_seconds": map[string]any{"type": "integer", "minimum": 0, "maximum": 900},
			},
			"",
		),
	}
}
