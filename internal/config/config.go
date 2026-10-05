package config

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	Addr              string
	Port              string
	DataDir           string
	StaticDir         string
	AppEncryptionKey  string
	AppEncryptionPath string
	// DownloadMinDelayMs is the lower bound (ms) of the random wait
	// between two consecutive chapter fetches. <= 0 means use the
	// downloader default.
	DownloadMinDelayMs int
	// DownloadMaxDelayMs is the upper bound (ms) of the random wait
	// between two consecutive chapter fetches. <= 0 means use the
	// downloader default.
	DownloadMaxDelayMs      int
	MigrateDB               bool
	MigrateChapterStats     bool
	MigrateChapterPositions bool
	// PromoteAdmin grants the admin role to the user with this email and
	// exits. Bootstrap for pre-existing installs that already have users
	// (fresh installs promote the first registrant automatically).
	PromoteAdmin string
	// PublicBaseURL is the externally reachable origin (e.g.
	// https://novels.example.com) used to build absolute URLs shown to
	// admins, such as invitation links. Empty means derive from the request
	// Host (dev only — Host is client-controlled on direct connections).
	PublicBaseURL string
	// BootstrapSecret is the one-time token a fresh install (zero users)
	// requires for its first, admin registration. Resolution happens in
	// main.go: -bootstrap-secret / BOOTSTRAP_SECRET, else
	// <data-dir>/setup.key, else a generated value persisted to that file.
	// Instances that already have users never consult it.
	BootstrapSecret string
	// ParsersDir is the directory scanned for site parser scripts (*.js).
	// Defaults to <data-dir>/parsers and is created on boot. There is no
	// cache: it is re-read per job, so editing a script takes effect on the
	// next download/check without a restart.
	ParsersDir string
	// CheckParser is the path to a single parser script to exercise against
	// CheckURL. Non-empty enables check mode, which runs before the server
	// starts and exits without touching the store.
	CheckParser string
	// CheckURL is the novel URL passed to the script's probe/toc/chapter in
	// check mode.
	CheckURL string
	// ParsersManifestURL is the release manifest (parsers/index.json) the
	// server fetches before running a parser script, to compare the installed
	// copy's sha256 against. Empty means this project's GitHub raw URL.
	ParsersManifestURL string
	// ParsersAutoUpdate enables the pre-execution parser update check that
	// downloads changed scripts from the release manifest. It comes from
	// PARSERS_AUTO_UPDATE (default enabled; "0"/"false" disables it) and
	// stays off when a Config is built by hand, so tests never reach the
	// network.
	ParsersAutoUpdate bool
	// ParsersAllowPrivateNets disables the SSRF guard that refuses direct
	// fetches to non-public IPs. It comes from PARSERS_ALLOW_PRIVATE_NETS
	// (default off) and exists for local development and tests, where site
	// hosts are rewritten onto 127.0.0.1 mocks. Never enable on an
	// internet-exposed server: parser scripts request untrusted URLs.
	ParsersAllowPrivateNets bool
}

func Load() (*Config, error) {
	cfg := &Config{}
	flag.StringVar(&cfg.Addr, "addr", "", "listen address")
	flag.StringVar(&cfg.Port, "port", "", "listen port")
	flag.StringVar(&cfg.DataDir, "data-dir", "", "data directory")
	flag.StringVar(&cfg.StaticDir, "static-dir", "", "dev static dir")
	flag.StringVar(&cfg.ParsersDir, "parsers-dir", "", "directory scanned for site parser scripts (*.js)")
	flag.BoolVar(&cfg.MigrateDB, "migrate-db", false, "migrate legacy database fields before serving")
	flag.BoolVar(&cfg.MigrateChapterStats, "migrate-chapter-stats", false, "recalculate chapter stats for every novel and exit")
	flag.BoolVar(&cfg.MigrateChapterPositions, "migrate-chapter-positions", false, "initialize chapter positions in source order and exit (required once before using chapter reorder/exclusion)")
	flag.StringVar(&cfg.PromoteAdmin, "promote-admin", "", "grant the admin role to the user with this email and exit")
	flag.StringVar(&cfg.PublicBaseURL, "public-url", "", "public origin for absolute URLs (e.g. https://novels.example.com)")
	flag.StringVar(&cfg.BootstrapSecret, "bootstrap-secret", "", "setup token required for the first registration on a fresh install (falls back to BOOTSTRAP_SECRET or <data-dir>/setup.key)")
	flag.StringVar(&cfg.CheckParser, "check-parser", "", "run a single parser script against -check-url, print the snapshot as JSON and exit")
	flag.StringVar(&cfg.CheckURL, "check-url", "", "novel URL used by -check-parser")
	flag.Parse()

	if cfg.Addr == "" {
		cfg.Addr = strings.TrimSpace(os.Getenv("ADDR"))
	}
	if cfg.Port == "" {
		cfg.Port = strings.TrimSpace(os.Getenv("PORT"))
	}
	if cfg.Addr == "" {
		port := strings.TrimSpace(cfg.Port)
		if port == "" {
			port = ":5176"
		} else if strings.HasPrefix(port, ":") {
			// port already has a leading colon
		} else {
			port = ":" + port
		}
		cfg.Addr = port
	}

	if cfg.DataDir == "" {
		cfg.DataDir = strings.TrimSpace(os.Getenv("DATA_DIR"))
	}
	if cfg.DataDir == "" {
		execPath, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("resolve executable path: %w", err)
		}
		cfg.DataDir = filepath.Join(filepath.Dir(execPath), "data")
	}
	absDataDir, err := filepath.Abs(cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("resolve data dir: %w", err)
	}
	cfg.DataDir = absDataDir
	cfg.AppEncryptionPath = filepath.Join(cfg.DataDir, "app.key")

	// Default the parser dir off the data dir so a fresh install has a
	// predictable place to drop scripts, then make it absolute like DataDir.
	parsersDir := cfg.ParsersDir
	if parsersDir == "" {
		parsersDir = strings.TrimSpace(os.Getenv("PARSERS_DIR"))
	}
	if parsersDir == "" {
		parsersDir = filepath.Join(cfg.DataDir, "parsers")
	}
	absParsersDir, err := filepath.Abs(parsersDir)
	if err != nil {
		return nil, fmt.Errorf("resolve parsers dir: %w", err)
	}
	cfg.ParsersDir = absParsersDir

	if cfg.StaticDir == "" {
		cfg.StaticDir = strings.TrimSpace(os.Getenv("STATIC_DIR"))
	}
	if cfg.StaticDir != "" {
		absStaticDir, err := filepath.Abs(cfg.StaticDir)
		if err != nil {
			return nil, fmt.Errorf("resolve static dir: %w", err)
		}
		cfg.StaticDir = absStaticDir
	}

	cfg.AppEncryptionKey = strings.TrimSpace(os.Getenv("APP_ENCRYPTION_KEY"))
	cfg.BootstrapSecret = firstNonEmpty(cfg.BootstrapSecret, strings.TrimSpace(os.Getenv("BOOTSTRAP_SECRET")))
	cfg.PublicBaseURL = firstNonEmpty(cfg.PublicBaseURL, strings.TrimSpace(os.Getenv("PUBLIC_URL")))
	cfg.PublicBaseURL = strings.TrimSuffix(cfg.PublicBaseURL, "/")
	cfg.DownloadMinDelayMs, cfg.DownloadMaxDelayMs = delayFromEnv()
	cfg.CheckParser = strings.TrimSpace(cfg.CheckParser)
	cfg.CheckURL = strings.TrimSpace(cfg.CheckURL)
	cfg.ParsersManifestURL = strings.TrimSpace(os.Getenv("PARSERS_MANIFEST_URL"))
	cfg.ParsersAutoUpdate = true
	if v := strings.TrimSpace(os.Getenv("PARSERS_AUTO_UPDATE")); v != "" {
		if enabled, err := strconv.ParseBool(v); err == nil {
			cfg.ParsersAutoUpdate = enabled
		} else {
			slog.Warn("ignoring invalid PARSERS_AUTO_UPDATE, keeping auto-update enabled", "value", v)
		}
	}
	if v := strings.TrimSpace(os.Getenv("PARSERS_ALLOW_PRIVATE_NETS")); v != "" {
		if enabled, err := strconv.ParseBool(v); err == nil {
			cfg.ParsersAllowPrivateNets = enabled
		} else {
			slog.Warn("ignoring invalid PARSERS_ALLOW_PRIVATE_NETS, keeping the SSRF guard enabled", "value", v)
		}
	}
	return cfg, nil
}

// delayFromEnv reads the inter-fetch throttle bounds from the environment.
// <= 0 (unset, empty or invalid) means the downloader default, as the deleted
// noveldownloader did: a random 5-10s gap between parser fetches.
func delayFromEnv() (int, int) {
	min, _ := strconv.Atoi(strings.TrimSpace(os.Getenv("DOWNLOAD_MIN_DELAY_MS")))
	max, _ := strconv.Atoi(strings.TrimSpace(os.Getenv("DOWNLOAD_MAX_DELAY_MS")))
	if min <= 0 {
		min = 5000
	}
	if max <= 0 {
		max = 10000
	}
	return min, max
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
