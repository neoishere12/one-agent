package mcp

func startLoginTool() mcpTool {
	return mcpTool{
		Name:        "start_login",
		Description: "Start async external login flow for Blinkit and return login_id + login_url.",
		InputSchema: objectSchema(
			map[string]any{
				"app":             enumString("blinkit"),
				"timeout_seconds": map[string]any{"type": "integer", "minimum": 0, "maximum": 900},
			},
			"app",
		),
	}
}

func loginStatusTool() mcpTool {
	return mcpTool{
		Name:        "login_status",
		Description: "Check status for an async login flow started by start_login.",
		InputSchema: objectSchema(
			map[string]any{
				"login_id": stringSchema(),
			},
			"login_id",
		),
	}
}
