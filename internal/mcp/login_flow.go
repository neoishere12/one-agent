package mcp

import (
	"sync"
	"time"

	"github.com/google/uuid"

	"one-agent/internal/types"
)

const (
	loginStatusPending   = "pending"
	loginStatusCompleted = "completed"
	loginStatusFailed    = "failed"
)

type loginFlowManager struct {
	mu      sync.RWMutex
	records map[string]*loginStatusOutput
	now     func() time.Time
}

func newLoginFlowManager(now func() time.Time) *loginFlowManager {
	if now == nil {
		now = time.Now
	}
	return &loginFlowManager{
		records: make(map[string]*loginStatusOutput),
		now:     now,
	}
}

func (m *loginFlowManager) create(app types.Platform, timeout time.Duration, loginURL, message string) loginStatusOutput {
	now := m.now().UTC()
	record := loginStatusOutput{
		LoginID:                uuid.New().String(),
		App:                    string(app),
		Status:                 loginStatusPending,
		Ready:                  false,
		NeedsHumanVerification: false,
		Message:                message,
		LoginURL:               loginURL,
		StatusTool:             "login_status",
		CreatedAt:              now,
		UpdatedAt:              now,
		ExpiresAt:              now.Add(timeout),
	}
	m.mu.Lock()
	m.records[record.LoginID] = &record
	m.mu.Unlock()
	return record
}

func (m *loginFlowManager) get(loginID string) (loginStatusOutput, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	record, ok := m.records[loginID]
	if !ok {
		return loginStatusOutput{}, false
	}
	expirePendingIfNeeded(record, m.now().UTC())
	return *record, true
}

func (m *loginFlowManager) pendingForApp(app types.Platform) (loginStatusOutput, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now().UTC()
	var latest *loginStatusOutput
	for _, record := range m.records {
		if record == nil {
			continue
		}
		expirePendingIfNeeded(record, now)
		if record.App != string(app) || record.Status != loginStatusPending {
			continue
		}
		if latest == nil || record.CreatedAt.After(latest.CreatedAt) {
			latest = record
		}
	}
	if latest == nil {
		return loginStatusOutput{}, false
	}
	return *latest, true
}

func (m *loginFlowManager) fail(loginID, message string, needsHuman bool) {
	m.update(loginID, func(record *loginStatusOutput, now time.Time) {
		record.Status = loginStatusFailed
		record.Ready = false
		record.NeedsHumanVerification = needsHuman
		record.Message = message
		record.UpdatedAt = now
	})
}

func (m *loginFlowManager) complete(loginID string, session *types.AppSession, message string) {
	if session == nil {
		m.fail(loginID, "login completed with empty session payload", false)
		return
	}
	m.update(loginID, func(record *loginStatusOutput, now time.Time) {
		record.Status = loginStatusCompleted
		record.Ready = true
		record.NeedsHumanVerification = false
		record.Message = message
		record.CapturedAt = session.CapturedAt
		record.TokenExpiresAt = session.ExpiresAt
		record.UpdatedAt = now
	})
}

func (m *loginFlowManager) update(loginID string, apply func(record *loginStatusOutput, now time.Time)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	record, ok := m.records[loginID]
	if !ok {
		return
	}
	apply(record, m.now().UTC())
}

func expirePendingIfNeeded(record *loginStatusOutput, now time.Time) {
	if record == nil || record.Status != loginStatusPending {
		return
	}
	if !record.ExpiresAt.IsZero() && now.After(record.ExpiresAt) {
		record.Status = loginStatusFailed
		record.Ready = false
		record.Message = "login session expired before completion"
		record.UpdatedAt = now
	}
}
