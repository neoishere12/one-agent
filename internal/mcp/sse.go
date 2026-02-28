package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"github.com/google/uuid"
)

// sseSession holds the response writer + channel for one connected Claude client.
type sseSession struct {
	id string
	ch chan rpcResponse
}

// sseHub manages active SSE sessions.
type sseHub struct {
	mu       sync.RWMutex
	sessions map[string]*sseSession
}

func newSSEHub() *sseHub {
	return &sseHub{sessions: make(map[string]*sseSession)}
}

func (h *sseHub) add(s *sseSession) {
	h.mu.Lock()
	h.sessions[s.id] = s
	h.mu.Unlock()
}

func (h *sseHub) remove(id string) {
	h.mu.Lock()
	delete(h.sessions, id)
	h.mu.Unlock()
}

func (h *sseHub) get(id string) (*sseSession, bool) {
	h.mu.RLock()
	s, ok := h.sessions[id]
	h.mu.RUnlock()
	return s, ok
}

// handleSSE handles GET /sse — establishes an SSE stream and sends the
// endpoint event so Claude knows where to POST requests.
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	sess := newSSESession()
	s.hub.add(sess)
	defer s.hub.remove(sess.id)
	openSSEStream(w, flusher, sess.id)
	s.streamSSESession(r.Context(), w, flusher, sess)
}

// handleMCPPost handles POST /mcp?session=<id>
// Claude posts JSON-RPC here; responses are sent back over SSE.
func (s *Server) handleMCPPost(w http.ResponseWriter, r *http.Request) {
	sessionID := sessionIDFromQuery(r)
	reqs, isBatch, reqErr := decodeRPCRequests(r.Body)
	if reqErr != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse(nil, reqErr))
		return
	}
	responses := s.executeRPCRequests(r.Context(), reqs)
	if s.dispatchToSSESession(sessionID, responses) {
		writeAccepted(w)
		return
	}
	writeRPCResponses(w, responses, isBatch)
}

func newSSESession() *sseSession {
	return &sseSession{
		id: uuid.New().String(),
		ch: make(chan rpcResponse, 32),
	}
}

func openSSEStream(w http.ResponseWriter, flusher http.Flusher, sessionID string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "event: endpoint\ndata: /messages?sessionId=%s\n\n", sessionID)
	flusher.Flush()
}

func (s *Server) streamSSESession(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, sess *sseSession) {
	for {
		select {
		case <-ctx.Done():
			return
		case resp, ok := <-sess.ch:
			if !ok {
				return
			}
			writeSSEMessage(w, flusher, resp)
		}
	}
}

func writeSSEMessage(w http.ResponseWriter, flusher http.Flusher, resp rpcResponse) {
	b, err := json.Marshal(resp)
	if err != nil {
		slog.Error("sse marshal error", "err", err)
		return
	}
	fmt.Fprintf(w, "event: message\ndata: %s\n\n", b)
	flusher.Flush()
}

func sessionIDFromQuery(r *http.Request) string {
	if id := r.URL.Query().Get("session"); id != "" {
		return id
	}
	return r.URL.Query().Get("sessionId")
}

func (s *Server) dispatchToSSESession(sessionID string, responses []rpcResponse) bool {
	if sessionID == "" {
		return false
	}
	sess, ok := s.hub.get(sessionID)
	if !ok {
		return false
	}
	for _, resp := range responses {
		select {
		case sess.ch <- resp:
		default:
			slog.Warn("sse session channel full, dropping response", "session", sessionID)
		}
	}
	return true
}
