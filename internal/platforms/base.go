package platforms

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"sync"
	"time"

	"one-agent/internal/store"
	"one-agent/internal/types"
)

// rateLimiter enforces max 1 request/second per platform (BELIEFS.md §5).
type rateLimiter struct {
	mu      sync.Mutex
	lastReq time.Time
	minGap  time.Duration
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{minGap: time.Second}
}

// wait blocks until it is safe to send the next request.
// Advances lastReq while holding the lock, sleeps outside it.
func (r *rateLimiter) wait(ctx context.Context) error {
	r.mu.Lock()
	gap := r.minGap - time.Since(r.lastReq)
	if gap <= 0 {
		r.lastReq = time.Now()
		r.mu.Unlock()
		return nil
	}
	r.lastReq = r.lastReq.Add(r.minGap)
	r.mu.Unlock()

	select {
	case <-time.After(gap):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// BaseClient is shared HTTP infrastructure for all platform clients:
// rate limiting, device header injection, auth, and request execution.
type BaseClient struct {
	st      *store.Store
	hc      *http.Client
	limiter *rateLimiter
	app     types.Platform
	BaseURL string // exported so Option functions can override it (e.g. in tests)
}

// NewBaseClient creates a BaseClient for the given platform app.
func NewBaseClient(s *store.Store, app types.Platform, baseURL string) *BaseClient {
	jar, err := cookiejar.New(nil)
	if err != nil {
		slog.Warn("failed to init cookie jar; continuing without cookie persistence", "app", app, "err", err)
	}
	return &BaseClient{
		st:      s,
		hc:      &http.Client{Timeout: 10 * time.Second, Jar: jar},
		limiter: newRateLimiter(),
		app:     app,
		BaseURL: baseURL,
	}
}

// RefreshFunc is the platform-specific token refresh implementation.
// Passed to LoadSession so BaseClient can refresh without knowing the platform.
type RefreshFunc func(ctx context.Context, refreshToken string) (newAccess, newRefresh string, expiresAt time.Time, err error)

// LoadSession reads the session from store and refreshes proactively if the
// token expires within 30 minutes (TOKENS.md expiry decision tree).
func (b *BaseClient) LoadSession(ctx context.Context, refresh RefreshFunc) (*types.AppSession, error) {
	sess, err := b.st.Get(ctx, b.app)
	if err != nil {
		return nil, fmt.Errorf("%s: load session: %w", b.app, err)
	}
	if sess.ExpiresAt.Before(time.Now().Add(30 * time.Minute)) {
		return b.doRefresh(ctx, sess, refresh)
	}
	return sess, nil
}

// LoadSessionRaw reads the session without any expiry check.
// Used by RefreshToken implementations that need device headers
// but are explicitly handling an expired token.
func (b *BaseClient) LoadSessionRaw(ctx context.Context) (*types.AppSession, error) {
	return b.st.Get(ctx, b.app)
}

// SaveSession persists the supplied session for this client's platform.
func (b *BaseClient) SaveSession(ctx context.Context, session *types.AppSession) error {
	return b.st.Set(ctx, b.app, session)
}

// doRefresh calls refresh, updates the session fields, and persists to store.
func (b *BaseClient) doRefresh(ctx context.Context, sess *types.AppSession, refresh RefreshFunc) (*types.AppSession, error) {
	slog.Info("token expiring — refreshing proactively",
		"app", b.app,
		"expires_at", sess.ExpiresAt.Format(time.RFC3339),
	)
	newAccess, newRefresh, expiresAt, err := refresh(ctx, sess.RefreshToken)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrSessionExpired, b.app)
	}
	sess.AccessToken = newAccess
	sess.RefreshToken = newRefresh
	sess.ExpiresAt = expiresAt
	sess.UpdatedAt = time.Now()
	if err := b.st.Set(ctx, b.app, sess); err != nil {
		return nil, fmt.Errorf("%s: persist refreshed session: %w", b.app, err)
	}
	slog.Info("token refreshed", "app", b.app,
		"new_expires_at", expiresAt.Format(time.RFC3339),
	)
	return sess, nil
}

// DoRequest rate-limits, injects auth and device headers, and executes the request.
// Callers must close the response Body.
func (b *BaseClient) DoRequest(
	ctx context.Context,
	method, path string,
	body any,
	sess *types.AppSession,
) (*http.Response, error) {
	return b.DoRequestWithBaseURL(ctx, method, b.BaseURL, path, body, sess)
}

// DoRequestWithBaseURL behaves like DoRequest, but allows callers to override
// the target origin while reusing the same auth/header/session flow.
func (b *BaseClient) DoRequestWithBaseURL(
	ctx context.Context,
	method, baseURL, path string,
	body any,
	sess *types.AppSession,
) (*http.Response, error) {
	if err := b.limiter.wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limit: %w", err)
	}
	req, err := b.buildRequest(ctx, method, baseURL, path, body, sess)
	if err != nil {
		return nil, err
	}
	resp, err := b.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", method, path, err)
	}
	return resp, nil
}

// buildRequest constructs the HTTP request with all required headers.
func (b *BaseClient) buildRequest(ctx context.Context, method, baseURL, path string, body any, sess *types.AppSession) (*http.Request, error) {
	buf := bytes.NewReader(nil)
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		buf = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, buf)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	// Inject frozen device headers captured from the live app (AGENTS.md constraints)
	for k, v := range sess.DeviceHeaders {
		req.Header.Set(k, v)
	}
	if b.app == types.PlatformBlinkit {
		webSession := isBlinkitWebSession(sess)
		if !hasHeader(sess.DeviceHeaders, "access_token") {
			req.Header.Set("access_token", sess.AccessToken)
		}
		if !hasHeader(sess.DeviceHeaders, "auth_key") {
			req.Header.Set("auth_key", sess.RefreshToken)
		}
		if !webSession {
			if !hasHeader(sess.DeviceHeaders, "req_key") {
				req.Header.Set("req_key", newReqKey())
			}
			if !hasHeader(sess.DeviceHeaders, "session_uuid") {
				req.Header.Set("session_uuid", newRequestID())
			}
		}
	} else {
		req.Header.Set("Authorization", "Bearer "+sess.AccessToken)
		req.Header.Set("x-request-id", newRequestID())
		req.Header.Set("x-session-id", newRequestID())
	}
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

func hasHeader(headers map[string]string, key string) bool {
	for k := range headers {
		if strings.EqualFold(k, key) {
			return true
		}
	}
	return false
}

func hasHeaderValue(headers map[string]string, key, want string) bool {
	for k, v := range headers {
		if strings.EqualFold(k, key) && strings.EqualFold(strings.TrimSpace(v), want) {
			return true
		}
	}
	return false
}

func isBlinkitWebSession(sess *types.AppSession) bool {
	if sess == nil {
		return false
	}
	return hasHeaderValue(sess.DeviceHeaders, "app_client", "consumer_web")
}

// CheckStatus converts a non-2xx HTTP status into a typed error.
// op is used for error context. Returns nil for 200/201/202.
func CheckStatus(resp *http.Response, platform, op string) error {
	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated, http.StatusAccepted:
		return nil
	case http.StatusUnauthorized:
		return fmt.Errorf("%w: %s %s", ErrSessionExpired, platform, op)
	case http.StatusForbidden:
		return fmt.Errorf("%w: %s %s", ErrDeviceRejected, platform, op)
	default:
		return fmt.Errorf("%s %s: unexpected status %d", platform, op, resp.StatusCode)
	}
}

// newRequestID generates a UUID-like random identifier for per-request tracing headers.
func newRequestID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func newReqKey() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
