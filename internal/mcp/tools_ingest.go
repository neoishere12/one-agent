package mcp

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"one-agent/internal/types"
)

// handleIngest processes POST /sessions/ingest from a capture importer.
// Returns true if the request was handled (regardless of outcome).
func (s *Server) handleIngest(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/sessions/ingest" {
		return false
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return true
	}
	if !s.allowIngest(r) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "rate limited"})
		return true
	}
	if !s.validateIngestSecret(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return true
	}
	var body ingestSessionInput
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return true
	}
	if err := s.writeIngestedSession(r.Context(), &body); err != nil {
		if isIngestValidationError(err) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid ingest payload"})
			return true
		}
		slog.Error("ingest: store write failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store write failed"})
		return true
	}
	slog.Info("ingest: session stored", "app", body.App)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	return true
}

// validateIngestSecret performs a constant-time comparison of the
// X-Ingest-Secret header against the configured secret.
func (s *Server) validateIngestSecret(r *http.Request) bool {
	got := r.Header.Get("X-Ingest-Secret")
	if s.ingestSecret == "" || got == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(s.ingestSecret)) == 1
}

// writeIngestedSession validates the ingest body and writes it to the store.
func (s *Server) writeIngestedSession(ctx context.Context, body *ingestSessionInput) error {
	app := types.Platform(body.App)
	switch app {
	case types.PlatformBlinkit, types.PlatformZepto, types.PlatformInstamart:
	default:
		return newIngestValidationError("unknown app")
	}
	if body.AccessToken == "" || body.RefreshToken == "" {
		return newIngestValidationError("missing token fields")
	}
	now := s.now()
	session := &types.AppSession{
		App:           app,
		AccessToken:   body.AccessToken,
		RefreshToken:  body.RefreshToken,
		ExpiresAt:     body.TokenExpiresAt,
		DeviceHeaders: body.DeviceHeaders,
		Addresses:     ingestAddressesToTypes(body.Addresses),
		Payments:      ingestPaymentsToTypes(body.Payments),
		CapturedAt:    now,
		UpdatedAt:     now,
	}
	if err := s.mergeIngestWithExisting(ctx, app, session); err != nil {
		return err
	}
	return s.st.Set(ctx, app, session)
}

func (s *Server) mergeIngestWithExisting(ctx context.Context, app types.Platform, incoming *types.AppSession) error {
	existing, err := s.st.Get(ctx, app)
	if err != nil {
		if isStoreNotFound(err) {
			return nil
		}
		return err
	}
	if len(incoming.DeviceHeaders) == 0 && len(existing.DeviceHeaders) > 0 {
		incoming.DeviceHeaders = existing.DeviceHeaders
	}
	if len(incoming.Addresses) == 0 && len(existing.Addresses) > 0 {
		incoming.Addresses = existing.Addresses
	}
	if len(incoming.Payments) == 0 && len(existing.Payments) > 0 {
		incoming.Payments = existing.Payments
	}
	return nil
}

type ingestValidationError struct {
	msg string
}

func (e ingestValidationError) Error() string { return e.msg }

func newIngestValidationError(msg string) error {
	return ingestValidationError{msg: msg}
}

func isIngestValidationError(err error) bool {
	var target ingestValidationError
	return errors.As(err, &target)
}

func ingestAddressesToTypes(in []ingestAddress) []types.Address {
	out := make([]types.Address, 0, len(in))
	for _, a := range in {
		out = append(out, types.Address{ID: a.ID, Label: a.Label, Line1: a.FullAddress})
	}
	return out
}

func ingestPaymentsToTypes(in []ingestPayment) []types.PaymentMethod {
	out := make([]types.PaymentMethod, 0, len(in))
	for _, p := range in {
		out = append(out, types.PaymentMethod{ID: p.ID, Label: p.Label, Token: p.Token, Type: p.Type})
	}
	return out
}
