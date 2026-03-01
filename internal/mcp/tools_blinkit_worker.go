package mcp

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"one-agent/internal/platforms/blinkit"
	"one-agent/internal/types"
)

const (
	defaultBlinkitWorkerStatusTimeout = 5 * time.Second
	defaultReverifyBlinkitTimeout     = 180 * time.Second
)

var (
	blinkitWorkerStatusSnapshotFn = blinkit.BrowserWorkerStatusSnapshot
	blinkitBootstrapWebSessionFn  = blinkit.BootstrapWebSession
)

func (s *Server) handleBlinkitWorkerStatus(ctx context.Context, raw []byte) (any, *toolError) {
	var input blinkitWorkerStatusInput
	if err := decodeParams(raw, &input); err != nil {
		return nil, err
	}
	if input.TimeoutSeconds < 0 {
		return nil, invalidParams("timeout_seconds must be >= 0", nil)
	}
	if input.TimeoutSeconds > 30 {
		return nil, invalidParams("timeout_seconds must be <= 30", nil)
	}

	timeout := defaultBlinkitWorkerStatusTimeout
	if input.TimeoutSeconds > 0 {
		timeout = time.Duration(input.TimeoutSeconds) * time.Second
	}
	status := s.fetchBlinkitWorkerStatus(ctx, timeout)
	return status, nil
}

func (s *Server) handleReverifyBlinkitSession(ctx context.Context, raw []byte) (any, *toolError) {
	var input reverifyBlinkitSessionInput
	if err := decodeParams(raw, &input); err != nil {
		return nil, err
	}
	if input.TimeoutSeconds < 0 {
		return nil, invalidParams("timeout_seconds must be >= 0", nil)
	}
	if input.TimeoutSeconds > 900 {
		return nil, invalidParams("timeout_seconds must be <= 900", nil)
	}

	timeout := defaultReverifyBlinkitTimeout
	if input.TimeoutSeconds > 0 {
		timeout = time.Duration(input.TimeoutSeconds) * time.Second
	}
	out := newReverifyOutput(ctx, s)
	if msg, blocked := reverifyBlockedReason(out.Worker); blocked {
		out.Message = msg
		return out, nil
	}

	session, err := runBlinkitReverify(ctx, timeout, s)
	if err != nil {
		out.Message = fmt.Sprintf("Blinkit reverify did not complete: %s", err.Error())
		if needsHumanVerificationError(err.Error()) {
			out.NeedsHumanVerification = true
		}
		return out, nil
	}
	return completeReverifyOutput(ctx, s, out, session), nil
}

func newReverifyOutput(ctx context.Context, s *Server) reverifyBlinkitSessionOutput {
	out := reverifyBlinkitSessionOutput{App: "blinkit", Ready: false}
	out.Worker = s.fetchBlinkitWorkerStatus(ctx, defaultBlinkitWorkerStatusTimeout)
	out.NeedsHumanVerification = out.Worker.NeedsHumanVerification
	return out
}

func runBlinkitReverify(ctx context.Context, timeout time.Duration, s *Server) (*types.AppSession, error) {
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return blinkitBootstrapWebSessionFn(runCtx, s.st)
}

func reverifyBlockedReason(worker blinkitWorkerStatusOutput) (string, bool) {
	if !worker.WorkerConfigured {
		return "BLINKIT_BROWSER_WORKER_URL is not set; configure worker routing before reverify", true
	}
	if !worker.WorkerReachable {
		return "Blinkit worker is unreachable; fix worker/tunnel connectivity and retry", true
	}
	if worker.ChallengeDetected {
		return "Blinkit challenge is active. Complete verification/login in the worker browser profile, then retry", true
	}
	return "", false
}

func needsHumanVerificationError(message string) bool {
	return strings.Contains(message, "human_verification_required") || strings.Contains(message, "access token not found")
}

func completeReverifyOutput(
	ctx context.Context,
	s *Server,
	out reverifyBlinkitSessionOutput,
	session *types.AppSession,
) reverifyBlinkitSessionOutput {
	out.Ready = true
	out.NeedsHumanVerification = false
	out.CapturedAt = session.CapturedAt
	out.TokenExpiresAt = session.ExpiresAt
	out.Message = "Blinkit browser session is verified and persisted"
	out.Worker = s.fetchBlinkitWorkerStatus(ctx, defaultBlinkitWorkerStatusTimeout)
	return out
}

func (s *Server) fetchBlinkitWorkerStatus(ctx context.Context, timeout time.Duration) blinkitWorkerStatusOutput {
	workerURL := strings.TrimSpace(os.Getenv("BLINKIT_BROWSER_WORKER_URL"))
	out := blinkitWorkerStatusOutput{
		WorkerConfigured: workerURL != "",
		WorkerURL:        workerURL,
	}
	if workerURL == "" {
		out.Message = "BLINKIT_BROWSER_WORKER_URL is not configured"
		return out
	}

	if timeout <= 0 {
		timeout = defaultBlinkitWorkerStatusTimeout
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	status, err := blinkitWorkerStatusSnapshotFn(probeCtx)
	if err != nil {
		out.WorkerReachable = false
		out.Message = fmt.Sprintf("worker status check failed: %v", err)
		return out
	}

	out.WorkerReachable = true
	out.PageURL = status.PageURL
	out.Title = status.Title
	out.AccessTokenPresent = status.AccessTokenPresent
	out.AuthKeyPresent = status.AuthKeyPresent
	out.ChallengeDetected = status.ChallengeDetected
	if status.ChallengeDetected {
		out.NeedsHumanVerification = true
		out.Message = "Blinkit challenge detected in browser profile"
		return out
	}
	if !status.AccessTokenPresent {
		out.NeedsHumanVerification = true
		out.Message = "Blinkit access token missing in browser profile"
		return out
	}
	out.NeedsHumanVerification = false
	out.Message = "Blinkit worker session looks ready"
	return out
}
