// Command gen-parser-manifest regenerates parsers/index.json, the release
// manifest the server fetches to auto-update installed parser scripts before
// running them. It loads every parser with the embedded engine — a script
// that does not load must fail here, before it can be published — and records
// its name, requiresBrowser flag and the sha256 of its content.
//
// Run it from the repository root: go run ./tools/gen-parser-manifest
// (or make parsers-manifest).
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"translator-server/internal/parserhost"
)

type manifestEntry struct {
	Name            string `json:"name"`
	File            string `json:"file"`
	SHA256          string `json:"sha256"`
	RequiresBrowser bool   `json:"requiresBrowser"`
}

type manifest struct {
	APIVersion int             `json:"apiVersion"`
	Parsers    []manifestEntry `json:"parsers"`
}

// noFetch is the Fetcher given to the loading engine. The script contract
// forbids fetches at module init, so if one attempts it, publishing must fail.
type noFetch struct{}

func (noFetch) Fetch(context.Context, string) (*parserhost.FetchResult, error) {
	return nil, fmt.Errorf("parser scripts must not fetch at module init")
}

func main() {
	if err := run(); err != nil {
		slog.Error("failed to generate parser manifest", "error", err)
		os.Exit(1)
	}
}

func run() error {
	const parsersDir = "parsers"
	entries, err := os.ReadDir(parsersDir)
	if err != nil {
		return fmt.Errorf("reading %s: %w", parsersDir, err)
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".js") {
			files = append(files, entry.Name())
		}
	}
	sort.Strings(files)

	engine := parserhost.NewEngine(noFetch{}, parserhost.Options{})
	out := manifest{APIVersion: parserhost.APIVersion, Parsers: make([]manifestEntry, 0, len(files))}
	for _, name := range files {
		path := filepath.Join(parsersDir, name)
		src, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}
		script, err := engine.LoadFile(path)
		if err != nil {
			return fmt.Errorf("loading %s: %w", path, err)
		}
		sum := sha256.Sum256(src)
		out.Parsers = append(out.Parsers, manifestEntry{
			Name:            script.Name(),
			File:            name,
			SHA256:          hex.EncodeToString(sum[:]),
			RequiresBrowser: script.RequiresBrowser(),
		})
	}

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding manifest: %w", err)
	}
	data = append(data, '\n')
	target := filepath.Join(parsersDir, "index.json")
	if err := os.WriteFile(target, data, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", target, err)
	}
	slog.Info("parser manifest written", "file", target, "parsers", len(out.Parsers))
	return nil
}
