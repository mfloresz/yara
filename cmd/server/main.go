package main

import (
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/pocketbase/pocketbase"
	"translator-server/internal/api"
	"translator-server/internal/config"
	"translator-server/internal/secure"
	"translator-server/internal/store"
)

var Version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	migrateThumbnails := flag.Bool("migrate-thumbnails", false, "generate thumbnails for all existing covers and exit")
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}
	if *showVersion {
		fmt.Println(Version)
		os.Exit(0)
	}

	encryptor, err := secure.NewEncryptorFromConfig(cfg.AppEncryptionKey, cfg.AppEncryptionPath)
	if err != nil {
		slog.Error("failed to create encryptor", "error", err)
		os.Exit(1)
	}

	app := pocketbase.NewWithConfig(pocketbase.Config{
		DefaultDataDir: cfg.DataDir,
		DefaultDev:     cfg.StaticDir != "",
	})
	if err := app.Bootstrap(); err != nil {
		slog.Error("failed to bootstrap pocketbase", "error", err)
		os.Exit(1)
	}

	st := store.New(app, encryptor)
	if err := st.EnsureSchema(); err != nil {
		slog.Error("failed to ensure schema", "error", err)
		os.Exit(1)
	}

	if *migrateThumbnails {
		slog.Info("running thumbnail migration")
		if err := st.RunThumbnailMigration(); err != nil {
			slog.Error("thumbnail migration failed", "error", err)
			os.Exit(1)
		}
		slog.Info("thumbnail migration finished, exiting")
		os.Exit(0)
	}

	if cfg.MigrateChapterStats {
		slog.Info("running chapter stats migration")
		if err := st.RunChapterStatsMigration(); err != nil {
			slog.Error("chapter stats migration failed", "error", err)
			os.Exit(1)
		}
		slog.Info("chapter stats migration finished, exiting")
		os.Exit(0)
	}

	server := api.New(st, cfg)
	handler := api.Router(server)

	slog.Info("translator-server listening", "addr", cfg.Addr, "dataDir", cfg.DataDir)
	if err := http.ListenAndServe(cfg.Addr, handler); err != nil {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
}
