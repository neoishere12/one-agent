package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"one-agent/internal/types"
)

const (
	defaultStartLoginTimeout = 300 * time.Second
	maxStartLoginTimeout     = 900 * time.Second
)

func (s *Server) handleStartLogin(ctx context.Context, raw []byte) (any, *toolError) {
	var input startLoginInput
	if err := decodeParams(raw, &input); err != nil {
		return nil, err
	}
	app, appErr := parsePlatform(strings.ToLower(strings.TrimSpace(input.App)))
	if appErr != nil {
		return nil, appErr
	}
	if app != types.PlatformBlinkit {
		return nil, invalidParams("start_login currently supports blinkit only", nil)
	}
	if pending, ok := s.loginFlows.pendingForApp(app); ok {
		pending.Message = "Blinkit login already in progress; keep polling login_status with existing login_id"
		return pending, nil
	}
	timeout, timeoutErr := parseStartLoginTimeout(input.TimeoutSeconds)
	if timeoutErr != nil {
		return nil, timeoutErr
	}
	output := s.loginFlows.create(app, timeout, "", startLoginMessage(""))
	loginURL := blinkitStartLoginURL(output.LoginID)
	output.LoginURL = loginURL
	output.Message = startLoginMessage(loginURL)
	s.loginFlows.update(output.LoginID, func(record *loginStatusOutput, now time.Time) {
		record.LoginURL = loginURL
		record.Message = output.Message
		record.UpdatedAt = now
	})
	worker := s.fetchBlinkitWorkerStatus(ctx, defaultBlinkitWorkerStatusTimeout)
	output.Worker = &worker
	if !worker.WorkerConfigured || !worker.WorkerReachable {
		msg := "Blinkit worker is not ready. Start blinkit-browser-worker and retry start_login"
		s.loginFlows.fail(output.LoginID, msg, worker.NeedsHumanVerification)
		failed, _ := s.loginFlows.get(output.LoginID)
		failed.Worker = &worker
		return failed, nil
	}
	go s.runBlinkitLoginFlow(output.LoginID, timeout)
	return output, nil
}

func (s *Server) handleLoginStatus(ctx context.Context, raw []byte) (any, *toolError) {
	var input loginStatusInput
	if err := decodeParams(raw, &input); err != nil {
		return nil, err
	}
	loginID := strings.TrimSpace(input.LoginID)
	if loginID == "" {
		return nil, invalidParams("login_id is required", nil)
	}
	out, ok := s.loginFlows.get(loginID)
	if !ok {
		return nil, invalidParams("login_id not found", errors.New(loginID))
	}
	if out.App == string(types.PlatformBlinkit) {
		worker := s.fetchBlinkitWorkerStatus(ctx, defaultBlinkitWorkerStatusTimeout)
		out.Worker = &worker
		if out.Status == loginStatusPending && worker.ChallengeDetected {
			out.NeedsHumanVerification = true
		}
	}
	return out, nil
}

func (s *Server) runBlinkitLoginFlow(loginID string, timeout time.Duration) {
	loginCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	session, err := blinkitBootstrapWebSessionFn(loginCtx, s.st)
	if err != nil {
		message := fmt.Sprintf("Blinkit login did not complete: %s", err.Error())
		s.loginFlows.fail(loginID, message, needsHumanVerificationError(err.Error()))
		return
	}
	s.loginFlows.complete(loginID, session, "Blinkit login completed and session persisted")
}

func parseStartLoginTimeout(seconds int) (time.Duration, *toolError) {
	if seconds < 0 {
		return 0, invalidParams("timeout_seconds must be >= 0", nil)
	}
	if seconds > int(maxStartLoginTimeout.Seconds()) {
		return 0, invalidParams("timeout_seconds must be <= 900", nil)
	}
	if seconds == 0 {
		return defaultStartLoginTimeout, nil
	}
	return time.Duration(seconds) * time.Second, nil
}

func startLoginMessage(loginURL string) string {
	if loginURL == "" {
		return "Complete Blinkit login in the VPS-controlled browser profile, then poll login_status with login_id. Set MCP_PUBLIC_BASE_URL plus BLINKIT_EXTERNAL_LOGIN_URL_TEMPLATE (or BLINKIT_EXTERNAL_LOGIN_URL) if you want a phone-openable login link."
	}
	return "Open login_url, complete Blinkit login in the linked remote browser, then poll login_status with login_id"
}
