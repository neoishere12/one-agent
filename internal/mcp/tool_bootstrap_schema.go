package mcp

func bootstrapBlinkitWebSessionTool() mcpTool {
	return mcpTool{
		Name:        "bootstrap_blinkit_web_session",
		Description: "Open/use Blinkit browser profile, wait for login session, and persist it into the store.",
		InputSchema: objectSchema(
			map[string]any{
				"timeout_seconds": map[string]any{"type": "integer", "minimum": 0, "maximum": 900},
			},
			"",
		),
	}
}
