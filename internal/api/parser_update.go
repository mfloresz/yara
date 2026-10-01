package api

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"translator-server/internal/parserhost"
)

// Parsers live in user-editable scripts, so a site fix must not require a
// Yara release. The release manifest — parsers/index.json in the project
// repository, served through GitHub raw — is the update channel: it lists
// every published parser with the sha256 of its current content. Before a
// script is executed, the installed copy's digest is compared against the
// manifest and a different version is downloaded, validated and atomically
// swapped in, so the job runs against the published parser rather than a
// stale local one.
const defaultParsersManifestURL = "https://raw.githubusercontent.com/mfloresz/yara/main/parsers/index.json"

const (
	// parserManifestTTL bounds how often a healthy install refetches the
	// manifest. It only throttles repeated successful executions: any parser
	// script failure invalidates the cache so the next attempt re-checks
	// immediately (the publish-fix-retry loop).
	parserManifestTTL = 5 * time.Minute
	// parserManifestFailureTTL stops an unreachable manifest host from
	// stalling every resolve: after a failed fetch, retries wait this long.
	parserManifestFailureTTL = time.Minute
	// parserUpdateTimeout bounds each manifest or script download.
	parserUpdateTimeout  = 10 * time.Second
	maxManifestBytes     = 1 << 20
	maxParserScriptBytes = 1 << 20
)

type parserManifestEntry struct {
	Name            string `json:"name"`
	File            string `json:"file"`
	SHA256          string `json:"sha256"`
	RequiresBrowser bool   `json:"requiresBrowser"`
}

type parserManifest struct {
	APIVersion int                   `json:"apiVersion"`
	Parsers    []parserManifestEntry `json:"parsers"`
	// Signature is the hex ed25519 signature over the canonical payload
	// (the {"apiVersion","parsers"} JSON object, compact encoding). It
	// proves the manifest was published with the project's signing key, so
	// a compromised mirror or MITM cannot ship malicious parser scripts to
	// installs. See verifyParserManifest and tools/sign-parsers-manifest.
	Signature string `json:"signature,omitempty"`
}

// defaultParsersManifestPubkey is the hex ed25519 public key the release
// manifest is signed with. Override per install with PARSERS_MANIFEST_PUBKEY
// (e.g. when pointing PARSERS_MANIFEST_URL at a fork signed with its own key).
const defaultParsersManifestPubkey = "c18acadcae8019f7757a4b7b074128bb7b6a39e5e285ae79b04157a3f44ee9de"

// manifestPayloadBytes renders the canonical signed payload: the manifest
// without its signature, compact JSON. The signing tool must produce byte-
// identical output for the same entries (same struct shape and field order).
func manifestPayloadBytes(manifest *parserManifest) ([]byte, error) {
	payload := struct {
		APIVersion int                   `json:"apiVersion"`
		Parsers    []parserManifestEntry `json:"parsers"`
	}{
		APIVersion: manifest.APIVersion,
		Parsers:    manifest.Parsers,
	}
	return json.Marshal(payload)
}

// verifyParserManifest rejects manifests that are not signed by the trusted
// key. An unsigned or badly signed manifest is treated like an unreachable
// one: the server logs a warning and runs what is installed. That keeps local
// development working (hand-edited mirrors, unsigned forks) while making the
// secure default explicit — auto-update only ever executes signed content.
func (s *Server) verifyParserManifest(manifest *parserManifest) error {
	pubHex := strings.TrimSpace(os.Getenv("PARSERS_MANIFEST_PUBKEY"))
	if pubHex == "" {
		pubHex = defaultParsersManifestPubkey
	}
	pub, err := hex.DecodeString(pubHex)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return fmt.Errorf("PARSERS_MANIFEST_PUBKEY is not a valid ed25519 public key")
	}
	if strings.TrimSpace(manifest.Signature) == "" {
		return fmt.Errorf("manifest is not signed; refusing auto-update (parsers will run as installed)")
	}
	sig, err := hex.DecodeString(strings.TrimSpace(manifest.Signature))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("manifest signature is not a valid ed25519 signature")
	}
	payload, err := manifestPayloadBytes(manifest)
	if err != nil {
		return fmt.Errorf("encoding manifest payload: %w", err)
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), payload, sig) {
		return fmt.Errorf("manifest signature verification failed; refusing auto-update")
	}
	return nil
}

// parserUpdateCache holds the fetched manifest and serializes parser file
// replacement. It is a value field on Server: the zero value is ready to use.
//
// The manifest itself is fetched WITHOUT holding mu (see currentManifest): a
// network call under the lock would serialize every parser execution behind a
// single up-to-10s request. Concurrent resolves share one in-flight fetch via
// the fetching channel instead.
type parserUpdateCache struct {
	mu        sync.Mutex
	manifest  *parserManifest
	fetchedAt time.Time
	failedAt  time.Time
	// fetching is non-nil while a manifest fetch is in flight; waiters block
	// on its close and then re-check the cache.
	fetching chan struct{}
}

func (s *Server) parsersAutoUpdate() bool {
	// Hand-built configs (tests) keep auto-update off so nothing reaches the
	// network; production enables it through config.Load.
	return s.Cfg != nil && s.Cfg.ParsersAutoUpdate
}

func (s *Server) parsersManifestURL() string {
	if s.Cfg != nil && s.Cfg.ParsersManifestURL != "" {
		return s.Cfg.ParsersManifestURL
	}
	return defaultParsersManifestURL
}

// currentManifest returns the cached release manifest, refetching it when the
// cache expired or was invalidated. Fetch failures are remembered briefly so
// an offline install does not stall every resolve on a doomed request.
//
// The HTTP fetch runs outside the cache mutex; concurrent callers coalesce
// onto the single in-flight fetch instead of stampeding the manifest host.
func (s *Server) currentManifest(ctx context.Context) (*parserManifest, error) {
	c := &s.parserUpdates
	for {
		c.mu.Lock()
		now := time.Now()
		if c.manifest != nil && now.Sub(c.fetchedAt) < parserManifestTTL {
			m := c.manifest
			c.mu.Unlock()
			return m, nil
		}
		if c.manifest == nil && !c.failedAt.IsZero() && now.Sub(c.failedAt) < parserManifestFailureTTL {
			c.mu.Unlock()
			return nil, fmt.Errorf("parser manifest fetch failed recently; retrying later")
		}
		if inFlight := c.fetching; inFlight != nil {
			c.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-inFlight:
				continue
			}
		}
		done := make(chan struct{})
		c.fetching = done
		c.mu.Unlock()

		manifest, err := fetchParserManifest(ctx, s.parsersManifestURL())
		if err == nil {
			err = s.verifyParserManifest(manifest)
		}

		c.mu.Lock()
		if err != nil {
			c.manifest = nil
			c.failedAt = time.Now()
			// Logged once per actual fetch, not per resolve: concurrent
			// waiters share the in-flight request and stay silent.
			slog.Warn("parser manifest fetch failed; parsers will run as installed", "url", s.parsersManifestURL(), "error", err)
		} else {
			c.manifest = manifest
			c.fetchedAt = time.Now()
			c.failedAt = time.Time{}
		}
		close(done)
		c.fetching = nil
		c.mu.Unlock()

		if err != nil {
			return nil, err
		}
		return manifest, nil
	}
}

// invalidateParserUpdates drops the cached manifest — including a remembered
// fetch failure — so the next parser execution re-checks it. Call sites are
// parser script failures: a repeatable failure is the signature of a stale
// installed script, and the manifest may already carry the published fix.
func (s *Server) invalidateParserUpdates() {
	c := &s.parserUpdates
	c.mu.Lock()
	c.manifest = nil
	c.fetchedAt = time.Time{}
	c.failedAt = time.Time{}
	c.mu.Unlock()
}

func fetchParserManifest(ctx context.Context, manifestURL string) (*parserManifest, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating manifest request: %w", err)
	}
	req.Header.Set("User-Agent", "yara-parser-updater")
	client := &http.Client{Timeout: parserUpdateTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching manifest: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching manifest: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxManifestBytes))
	if err != nil {
		return nil, fmt.Errorf("reading manifest: %w", err)
	}
	var manifest parserManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return nil, fmt.Errorf("parsing manifest: %w", err)
	}
	if manifest.APIVersion != parserhost.APIVersion {
		return nil, fmt.Errorf("manifest apiVersion %d not supported (want %d)", manifest.APIVersion, parserhost.APIVersion)
	}
	for _, entry := range manifest.Parsers {
		if err := validateManifestEntry(entry); err != nil {
			return nil, fmt.Errorf("manifest entry: %w", err)
		}
	}
	return &manifest, nil
}

// validateManifestEntry guards against manifest entries that would write
// outside the parsers directory or fetch anything that is not a bare script
// file name.
func validateManifestEntry(entry parserManifestEntry) error {
	if strings.TrimSpace(entry.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if entry.File == "" || entry.File != filepath.Base(entry.File) ||
		!strings.HasSuffix(entry.File, ".js") || strings.ContainsAny(entry.File, `/\`) {
		return fmt.Errorf("file %q is not a bare .js file name", entry.File)
	}
	if !validSHA256(entry.SHA256) {
		return fmt.Errorf("sha256 %q is not a hex digest", entry.SHA256)
	}
	return nil
}

func validSHA256(s string) bool {
	if len(s) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

// parserFileURL derives the raw script URL from the manifest URL: the
// manifest is <base>/index.json, so scripts live next to it.
func parserFileURL(manifestURL, file string) (string, error) {
	base, ok := strings.CutSuffix(manifestURL, "index.json")
	if !ok || base == "" {
		return "", fmt.Errorf("manifest URL must end with index.json: %s", manifestURL)
	}
	return base + file, nil
}

// resolveScriptForExecution is resolveScriptForURL for flows that are about
// to run a parser (a TOC fetch, a chapter download or a whole download job).
// The selected script's digest is checked against the release manifest and a
// different version is downloaded in place before running; when nothing
// claims the URL at all, scripts listed in the manifest but missing locally
// (fresh installs start with an empty directory) are downloaded once and
// selection retried. Read-only lookups (canUpdate, requiresBrowser) keep using
// resolveScriptForURL and never trigger downloads.
//
// Scoping is deliberate: parsers are per host, so a failure on one site never
// refreshes or retries unrelated scripts. Only the selected script is ever
// updated; anything else is a hard failure for the user to see.
func (s *Server) resolveScriptForExecution(ctx context.Context, userID, rawURL string) (parserScript, error) {
	entry, err := s.resolveScriptForURL(userID, rawURL)
	if err != nil {
		var scriptErr *parserhost.ScriptError
		if errors.As(err, &scriptErr) && scriptErr.Code == parserhost.CodeNotMySite && s.ensureMissingParsers(ctx, userID) {
			return s.resolveScriptForURL(userID, rawURL)
		}
		return parserScript{}, err
	}
	if s.refreshParserScript(ctx, userID, entry.file) {
		return s.resolveScriptForURL(userID, rawURL)
	}
	return entry, nil
}

// refreshParserScript compares the script file the current execution selected
// against the release manifest and reports whether the file was replaced.
func (s *Server) refreshParserScript(ctx context.Context, userID, scriptFile string) bool {
	if !s.parsersAutoUpdate() || scriptFile == "" {
		return false
	}
	manifest, err := s.currentManifest(ctx)
	if err != nil {
		return false // already logged at fetch time
	}
	entry := findParserManifestEntry(manifest, filepath.Base(scriptFile))
	if entry == nil {
		return false // not in the release catalog: a locally added script
	}
	if local, err := os.ReadFile(scriptFile); err == nil && sha256Hex(local) == entry.SHA256 {
		return false // already the published version
	}
	return s.applyParserUpdate(ctx, userID, *entry)
}

// updatedScriptForRetry checks the selected script against the release
// manifest and, when a newer version was swapped in, returns it routed for
// execution so the caller can retry the failed call once. retried=false means
// no update was available (up to date, not in the manifest, auto-update off
// or manifest unreachable) and the caller should report its original error.
//
// This is the "si falla el parser, se busca actualizar o se emite falla"
// path: only the selected script is ever touched, never the other sites'
// scripts.
func (s *Server) updatedScriptForRetry(ctx context.Context, userID string, entry parserScript, rawURL string) (*parserhost.Script, bool) {
	if !s.refreshParserScript(ctx, userID, entry.file) {
		return nil, false
	}
	// The manifest was just refreshed by the update above; re-resolve to pick
	// up the swapped file.
	fresh, err := s.resolveScriptForURL(userID, rawURL)
	if err != nil {
		return nil, false
	}
	script, err := s.routedScript(userID, fresh)
	if err != nil {
		return nil, false
	}
	return script, true
}

// ensureMissingParsers downloads the manifest scripts that are not installed
// locally and reports whether any file was added. It backs the one-shot retry
// after a not_my_site selection on fresh installs (which start with an empty
// directory). Unlike a full refresh it never re-downloads scripts that are
// already installed: parsers are per host, so updating unrelated sites cannot
// fix a URL none of them claims.
func (s *Server) ensureMissingParsers(ctx context.Context, userID string) bool {
	if !s.parsersAutoUpdate() {
		return false
	}
	dir := s.parsersDir()
	if dir == "" {
		return false
	}
	manifest, err := s.currentManifest(ctx)
	if err != nil {
		return false
	}
	changed := false
	for _, entry := range manifest.Parsers {
		if _, err := os.Stat(filepath.Join(dir, entry.File)); err == nil {
			continue // installed (fresh or stale — staleness is the
			// selected script's update path, not this one's)
		} else if !os.IsNotExist(err) {
			continue // unreadable path: leave it alone
		}
		if s.applyParserUpdate(ctx, userID, entry) {
			changed = true
		}
	}
	return changed
}

// applyParserUpdate downloads one script named by the manifest, validates it
// (digest, compile, contract shape) and atomically replaces the local copy.
// The download runs without holding the cache mutex; only the final rename is
// serialized, with a fresh digest re-check so two jobs racing on the same
// parser cannot interleave writes.
func (s *Server) applyParserUpdate(ctx context.Context, userID string, entry parserManifestEntry) bool {
	dir := s.parsersDir()
	if dir == "" {
		return false
	}
	target := filepath.Join(dir, entry.File)

	scriptURL, err := parserFileURL(s.parsersManifestURL(), entry.File)
	if err != nil {
		slog.Warn("parser update skipped; unusable manifest URL", "parser", entry.Name, "error", err)
		return false
	}
	scriptBytes, err := fetchParserScript(ctx, scriptURL)
	if err != nil {
		slog.Warn("parser update download failed; keeping installed script", "parser", entry.Name, "file", entry.File, "error", err)
		return false
	}
	if sha256Hex(scriptBytes) != entry.SHA256 {
		slog.Warn("downloaded parser script does not match the manifest digest; keeping installed script", "parser", entry.Name, "file", entry.File)
		return false
	}

	tmp, err := os.CreateTemp(dir, ".yara-update-*.tmp")
	if err != nil {
		slog.Warn("parser update could not stage a temp file; keeping installed script", "parser", entry.Name, "file", entry.File, "error", err)
		return false
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename succeeded
	if _, err := tmp.Write(scriptBytes); err != nil {
		tmp.Close()
		slog.Warn("parser update could not write the staged file; keeping installed script", "parser", entry.Name, "file", entry.File, "error", err)
		return false
	}
	if err := tmp.Close(); err != nil {
		slog.Warn("parser update could not flush the staged file; keeping installed script", "parser", entry.Name, "file", entry.File, "error", err)
		return false
	}
	// Compile plus contract-shape validation before anything replaces the
	// installed copy: a broken download must never become the running script.
	if _, err := s.newParserEngine(userID, false, nil).LoadFile(tmpName); err != nil {
		slog.Warn("downloaded parser script failed validation; keeping installed script", "parser", entry.Name, "file", entry.File, "error", err)
		return false
	}

	c := &s.parserUpdates
	c.mu.Lock()
	defer c.mu.Unlock()
	// A concurrent refresh may have installed the same file already.
	if local, err := os.ReadFile(target); err == nil && sha256Hex(local) == entry.SHA256 {
		return false
	}
	if err := os.Rename(tmpName, target); err != nil {
		slog.Warn("parser update replacement failed; keeping installed script", "parser", entry.Name, "file", entry.File, "error", err)
		return false
	}
	slog.Info("parser script updated from the release manifest", "parser", entry.Name, "file", entry.File)
	return true
}

func fetchParserScript(ctx context.Context, scriptURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, scriptURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating script request: %w", err)
	}
	req.Header.Set("User-Agent", "yara-parser-updater")
	client := &http.Client{Timeout: parserUpdateTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", scriptURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching %s: status %d", scriptURL, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxParserScriptBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", scriptURL, err)
	}
	if int64(len(body)) > maxParserScriptBytes {
		return nil, fmt.Errorf("fetching %s: script exceeds %d bytes", scriptURL, maxParserScriptBytes)
	}
	return body, nil
}

func findParserManifestEntry(manifest *parserManifest, file string) *parserManifestEntry {
	for i := range manifest.Parsers {
		if manifest.Parsers[i].File == file {
			return &manifest.Parsers[i]
		}
	}
	return nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
