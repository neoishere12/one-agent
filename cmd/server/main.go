package main

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"one-agent/internal/config"
	"one-agent/internal/mcp"
	"one-agent/internal/platforms"
	"one-agent/internal/platforms/blinkit"
	"one-agent/internal/platforms/instamart"
	"one-agent/internal/platforms/zepto"
	"one-agent/internal/store"
	"one-agent/internal/types"
)

func main() {
	if err := runServer(); err != nil {
		slog.Error("server command failed", "err", err)
		os.Exit(1)
	}
}

func runServer() error {
	cfg := config.Load()
	st, err := store.New(cfg)
	if err != nil {
		return fmt.Errorf("init store: %w", err)
	}
	defer func() { _ = st.Close() }()

	mcpHandler := mcp.New(
		st,
		newPlatformClients(st),
		mcp.WithIngestSecret(cfg.IngestSecret),
		mcp.WithIngestRateLimit(cfg.IngestRateLimitMax, cfg.IngestRateLimitWindow),
	)

	mux := http.NewServeMux()
	mux.Handle("/rpc", mcpHandler)
	mux.Handle("/mcp", mcpHandler)
	mux.Handle("/sse", mcpHandler)
	mux.Handle("/messages", mcpHandler)
	mux.Handle("/message", mcpHandler)
	mux.Handle("/health", mcpHandler)
	mux.Handle("/sessions/ingest", mcpHandler)

	httpServer := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.MCPPort),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	slog.Info("mcp server listening", "addr", httpServer.Addr)

	err = httpServer.ListenAndServe()
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return fmt.Errorf("listen: %w", err)
}

func newPlatformClients(st *store.Store) map[types.Platform]platforms.Platform {
	return map[types.Platform]platforms.Platform{
		types.PlatformBlinkit:   blinkit.New(st),
		types.PlatformZepto:     zepto.New(st),
		types.PlatformInstamart: instamart.New(st),
	}
}
