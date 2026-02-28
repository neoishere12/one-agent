package mcp

import (
	"context"
	"errors"
	"strings"
	"time"

	"one-agent/internal/refresh"
	"one-agent/internal/store"
	"one-agent/internal/types"
)

// handleCaptureSession polls the store until the iOS app delivers a new session
// via POST /sessions/ingest, or the 5-minute timeout expires.
func (s *Server) handleCaptureSession(ctx context.Context, raw []byte) (any, *toolError) {
	var input captureSessionInput
	if err := decodeParams(raw, &input); err != nil {
		return nil, err
	}
	app, parseErr := parsePlatform(strings.ToLower(strings.TrimSpace(input.App)))
	if parseErr != nil {
		return nil, parseErr
	}
	start := s.now()
	pollCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	session, err := s.pollForSession(pollCtx, app, start)
	if err != nil {
		return nil, internalError(
			"capture_session timed out — capture traffic in Proxyman, export HAR, and run proxyman-import",
			err,
		)
	}
	return captureSessionOutput{
		App:                 string(app),
		CapturedAt:          session.CapturedAt,
		AddressesFound:      len(session.Addresses),
		PaymentMethodsFound: len(session.Payments),
		TokenExpiresAt:      session.ExpiresAt,
	}, nil
}

// pollForSession checks the store every 2 seconds until a session with
// CapturedAt after `after` is found, or the context is cancelled.
func (s *Server) pollForSession(ctx context.Context, app types.Platform, after time.Time) (*types.AppSession, error) {
	for {
		session, err := s.st.Get(ctx, app)
		if err == nil && session.CapturedAt.After(after) {
			return session, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func (s *Server) handleListSessions(ctx context.Context) (any, *toolError) {
	apps, err := s.st.List(ctx)
	if err != nil {
		return nil, internalError("list_sessions failed", err)
	}
	summaries := make([]sessionSummary, 0, len(apps))
	for _, app := range apps {
		summary, buildErr := s.buildSessionSummary(ctx, app)
		if buildErr != nil {
			return nil, buildErr
		}
		summaries = append(summaries, summary)
	}
	return listSessionsOutput{Sessions: summaries}, nil
}

func (s *Server) buildSessionSummary(ctx context.Context, app types.Platform) (sessionSummary, *toolError) {
	session, err := s.st.Get(ctx, app)
	if err != nil {
		return sessionSummary{}, internalError("list_sessions failed", err)
	}
	return sessionSummary{
		App:            string(app),
		CapturedAt:     session.CapturedAt,
		TokenExpiresAt: session.ExpiresAt,
		TokenValid:     session.ExpiresAt.After(s.now()),
		Addresses:      addressLabels(session),
		PaymentMethods: paymentLabels(session),
	}, nil
}

func (s *Server) handleRefreshTokens(ctx context.Context, raw []byte) (any, *toolError) {
	var input refreshTokensInput
	if err := decodeParams(raw, &input); err != nil {
		return nil, err
	}
	if input.App == nil {
		return s.refreshAll(ctx)
	}
	return s.refreshOneApp(ctx, strings.ToLower(strings.TrimSpace(*input.App)))
}

func (s *Server) refreshAll(ctx context.Context) (any, *toolError) {
	results, err := refresh.RefreshAll(ctx, s.st, s.platforms)
	output := buildRefreshOutput(results)
	if err != nil {
		return output, internalError("refresh_tokens failed", err)
	}
	return output, nil
}

func (s *Server) refreshOneApp(ctx context.Context, appValue string) (any, *toolError) {
	app, appErr := parsePlatform(appValue)
	if appErr != nil {
		return nil, appErr
	}
	client, getErr := s.platform(app)
	if getErr != nil {
		return nil, getErr
	}
	result, refreshErr := s.refreshOne(ctx, app, client)
	if refreshErr != nil {
		out := buildRefreshOutput(map[types.Platform]refresh.Result{app: result})
		return out, refreshErr
	}
	return buildRefreshOutput(map[types.Platform]refresh.Result{app: result}), nil
}

func buildRefreshOutput(results map[types.Platform]refresh.Result) refreshTokensOutput {
	out := refreshTokensOutput{
		Refreshed: make([]string, 0, len(results)),
		Failed:    make([]string, 0, len(results)),
		Results:   make(map[string]refreshResult, len(results)),
	}
	for app, result := range results {
		key := string(app)
		entry := refreshResult{Success: result.Success}
		if result.Success {
			entry.NewExpiry = result.NewExpiry
			out.Refreshed = append(out.Refreshed, key)
		} else {
			entry.Error = errString(result.Err)
			out.Failed = append(out.Failed, key)
		}
		out.Results[key] = entry
	}
	return out
}

func addressLabels(session *types.AppSession) []string {
	out := make([]string, 0, len(session.Addresses))
	for _, address := range session.Addresses {
		if address.Label != "" {
			out = append(out, address.Label)
			continue
		}
		out = append(out, address.ID)
	}
	return out
}

func paymentLabels(session *types.AppSession) []string {
	out := make([]string, 0, len(session.Payments))
	for _, payment := range session.Payments {
		if payment.Label != "" {
			out = append(out, payment.Label)
			continue
		}
		out = append(out, payment.ID)
	}
	return out
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func isStoreNotFound(err error) bool {
	return errors.Is(err, store.ErrNotFound)
}

func deadlineContext(parent context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, d)
}
