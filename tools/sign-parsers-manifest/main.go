// Command sign-parsers-manifest signs parsers/index.json with the project's
// ed25519 parser-signing key, so installs can verify the auto-update manifest
// before executing downloaded scripts (see verifyParserManifest in
// internal/api/parser_update.go).
//
// Usage from the repository root:
//
//	go run ./tools/sign-parsers-manifest            # sign with key from env/file
//	go run ./tools/sign-parsers-manifest -keygen    # print a fresh keypair
//
// The private key is read from PARSERS_SIGNING_KEY (hex seed, 64 chars) or
// from .parsers-signing.key in the repo root (gitignored, mode 0600). The
// matching public key is embedded in the server binary (see
// defaultParsersManifestPubkey) or overridden per install with
// PARSERS_MANIFEST_PUBKEY.
//
// The signed payload is the compact JSON {"apiVersion":..,"parsers":[..]} —
// byte-identical to what the server's manifestPayloadBytes produces. The
// signature is stored as hex in the manifest's top-level "signature" field;
// the file itself stays pretty-printed.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
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
	Signature  string          `json:"signature,omitempty"`
}

func main() {
	keygen := flag.Bool("keygen", false, "generate a fresh signing keypair and exit")
	flag.Parse()
	if *keygen {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			slog.Error("generating keypair", "error", err)
			os.Exit(1)
		}
		fmt.Printf("public (embed as defaultParsersManifestPubkey or PARSERS_MANIFEST_PUBKEY):\n%s\n\n", hex.EncodeToString(pub))
		fmt.Printf("private seed (PARSERS_SIGNING_KEY or .parsers-signing.key, keep secret):\n%s\n", hex.EncodeToString(priv.Seed()))
		return
	}
	if err := run(); err != nil {
		slog.Error("failed to sign parser manifest", "error", err)
		os.Exit(1)
	}
}

func run() error {
	seed, err := loadSigningSeed()
	if err != nil {
		return err
	}
	priv := ed25519.NewKeyFromSeed(seed)

	target := filepath.Join("parsers", "index.json")
	raw, err := os.ReadFile(target)
	if err != nil {
		return fmt.Errorf("reading %s: %w", target, err)
	}
	var m manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return fmt.Errorf("parsing %s: %w", target, err)
	}
	payload, err := json.Marshal(struct {
		APIVersion int             `json:"apiVersion"`
		Parsers    []manifestEntry `json:"parsers"`
	}{APIVersion: m.APIVersion, Parsers: m.Parsers})
	if err != nil {
		return fmt.Errorf("encoding manifest payload: %w", err)
	}
	m.Signature = hex.EncodeToString(ed25519.Sign(priv, payload))

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding manifest: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(target, data, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", target, err)
	}
	slog.Info("parser manifest signed", "file", target, "parsers", len(m.Parsers))
	return nil
}

// loadSigningSeed reads the 32-byte ed25519 seed from PARSERS_SIGNING_KEY
// (preferred, e.g. in CI) or from .parsers-signing.key in the repo root.
func loadSigningSeed() ([]byte, error) {
	hexSeed := strings.TrimSpace(os.Getenv("PARSERS_SIGNING_KEY"))
	if hexSeed == "" {
		raw, err := os.ReadFile(".parsers-signing.key")
		if err != nil {
			return nil, fmt.Errorf("no signing key: set PARSERS_SIGNING_KEY or create .parsers-signing.key (see -keygen)")
		}
		hexSeed = strings.TrimSpace(string(raw))
	}
	decoded, err := hex.DecodeString(hexSeed)
	if err != nil || len(decoded) != ed25519.SeedSize {
		return nil, fmt.Errorf("signing key is not a 32-byte hex seed")
	}
	return decoded, nil
}
