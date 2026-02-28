package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"one-agent/internal/proxyman"
	"one-agent/internal/types"
)

type watcher struct {
	cfg cliConfig
}

func newWatcher(cfg cliConfig) watcher {
	return watcher{cfg: cfg}
}

func ensureDirs(cfg cliConfig) error {
	if err := os.MkdirAll(cfg.watchDir, 0o755); err != nil {
		return fmt.Errorf("mkdir watch dir: %w", err)
	}
	if err := os.MkdirAll(cfg.processedDir, 0o755); err != nil {
		return fmt.Errorf("mkdir processed dir: %w", err)
	}
	if err := os.MkdirAll(cfg.failedDir, 0o755); err != nil {
		return fmt.Errorf("mkdir failed dir: %w", err)
	}
	return nil
}

func (w watcher) runLoop(ctx context.Context) error {
	for {
		count, err := w.scanOnce(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "scan error: %v\n", err)
		}
		if count > 0 {
			fmt.Printf("scan complete: processed=%d\n", count)
		}
		if err := waitOrStop(ctx, w.cfg.pollInterval); err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return err
		}
	}
}

func waitOrStop(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (w watcher) scanOnce(ctx context.Context) (int, error) {
	paths, err := candidateFiles(w.cfg.watchDir, w.cfg.settleDuration)
	if err != nil {
		return 0, err
	}
	processed := 0
	for _, path := range paths {
		if ctx.Err() != nil {
			return processed, nil
		}
		if err := w.handleFile(ctx, path); err != nil {
			fmt.Fprintf(os.Stderr, "failed %s: %v\n", filepath.Base(path), err)
		}
		processed++
	}
	return processed, nil
}

func candidateFiles(dir string, settle time.Duration) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read watch dir: %w", err)
	}
	now := time.Now()
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !isCandidateEntry(entry.Name(), entry.IsDir()) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if now.Sub(info.ModTime()) < settle {
			continue
		}
		out = append(out, filepath.Join(dir, entry.Name()))
	}
	sort.Strings(out)
	return out, nil
}

func isCandidateEntry(name string, isDir bool) bool {
	if isDir {
		return false
	}
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".har") || strings.HasSuffix(lower, ".proxymanv2")
}

func (w watcher) handleFile(ctx context.Context, path string) error {
	err := w.processFile(ctx, path)
	if err != nil {
		moveErr := archiveFile(path, w.cfg.failedDir, ".failed")
		if moveErr != nil {
			return fmt.Errorf("%w; archive failure: %v", err, moveErr)
		}
		_ = writeFailureNote(path, w.cfg.failedDir, err)
		return err
	}
	return archiveFile(path, w.cfg.processedDir, ".ok")
}

func writeFailureNote(srcPath, failedDir string, processErr error) error {
	noteName := destinationName(srcPath, ".failed.error.txt")
	notePath := filepath.Join(failedDir, noteName)
	content := fmt.Sprintf("%s\n", processErr.Error())
	return os.WriteFile(notePath, []byte(content), 0o644)
}

func archiveFile(srcPath, dstDir, suffix string) error {
	dstPath := filepath.Join(dstDir, destinationName(srcPath, suffix))
	if err := os.Rename(srcPath, dstPath); err == nil {
		return nil
	}
	if err := copyAndRemove(srcPath, dstPath); err != nil {
		return fmt.Errorf("archive file: %w", err)
	}
	return nil
}

func destinationName(srcPath, suffix string) string {
	stamp := time.Now().UTC().Format("20060102T150405Z")
	base := filepath.Base(srcPath)
	return fmt.Sprintf("%s%s_%s", stamp, suffix, base)
}

func copyAndRemove(srcPath, dstPath string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		return err
	}
	if err := dst.Close(); err != nil {
		return err
	}
	return os.Remove(srcPath)
}

func (w watcher) processFile(ctx context.Context, path string) error {
	if strings.HasSuffix(strings.ToLower(path), ".proxymanv2") {
		return fmt.Errorf("unsupported format .proxymanv2; export HAR from Proxyman")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}
	app, err := resolveApp(w.cfg, path, data)
	if err != nil {
		return err
	}
	payload, diag, err := parsePayload(w.cfg, app, data)
	if w.cfg.diagnostics && diag != nil {
		printDiagnostics(*diag)
	}
	if err != nil {
		return err
	}
	printSummary(proxyman.Summarize(payload), w.cfg.dryRun)
	if w.cfg.dryRun {
		return nil
	}
	postCtx, cancel := context.WithTimeout(ctx, w.cfg.timeout)
	defer cancel()
	return proxyman.PostIngest(postCtx, payload, proxyman.PostOptions{
		URL:    w.cfg.ingestURL,
		Secret: w.cfg.ingestSecret,
	})
}

func resolveApp(cfg cliConfig, path string, data []byte) (types.Platform, error) {
	if cfg.app != "" {
		return cfg.app, nil
	}
	app, err := proxyman.DetectApp(filepath.Base(path), bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("auto-detect app failed for %s: %w", filepath.Base(path), err)
	}
	return app, nil
}

func parsePayload(
	cfg cliConfig,
	app types.Platform,
	data []byte,
) (*proxyman.IngestPayload, *proxyman.ParseDiagnostics, error) {
	payload, diag, err := proxyman.ParseHARWithDiagnostics(bytes.NewReader(data), proxyman.ParseOptions{
		App:             app,
		TokenExpiresAt:  cfg.tokenExpiresAt,
		DefaultTokenTTL: cfg.defaultTokenTTL,
	})
	if err != nil {
		return nil, diag, err
	}
	return payload, diag, nil
}
