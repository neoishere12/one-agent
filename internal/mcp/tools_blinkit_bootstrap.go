package mcp

import (
	"context"
	"fmt"
	"time"

	"one-agent/internal/platforms/blinkit"
)

func (s *Server) handleBootstrapBlinkitWebSession(ctx context.Context, raw []byte) (any, *toolError) {
	var input bootstrapBlinkitWebSessionInput
	if err := decodeParams(raw, &input); err != nil {
		return nil, err
	}
	if input.TimeoutSeconds < 0 {
		return nil, invalidParams("timeout_seconds must be >= 0", nil)
	}
	if input.TimeoutSeconds > 900 {
		return nil, invalidParams("timeout_seconds must be <= 900", nil)
	}

	runCtx := ctx
	cancel := func() {}
	if input.TimeoutSeconds > 0 {
		runCtx, cancel = context.WithTimeout(ctx, time.Duration(input.TimeoutSeconds)*time.Second)
	}
	defer cancel()

	session, err := blinkit.BootstrapWebSession(runCtx, s.st)
	if err != nil {
		return nil, internalError(
			"bootstrap_blinkit_web_session failed",
			fmt.Errorf("complete Blinkit login in browser profile and retry: %w", err),
		)
	}
	return bootstrapBlinkitWebSessionOutput{
		App:               "blinkit",
		CapturedAt:        session.CapturedAt,
		TokenExpiresAt:    session.ExpiresAt,
		TokenValid:        session.ExpiresAt.After(s.now()),
		DeviceHeaderCount: len(session.DeviceHeaders),
	}, nil
}
