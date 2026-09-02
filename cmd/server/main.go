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

	// Defense in depth for internet exposure: PB's own superuser whitelist
	// middleware only restricts superuser-authenticated requests when the
	// list is non-empty, so default it to loopback. The /_/ dashboard path is
	// answered 404 by the router middleware regardless.
	if len(app.Settings().SuperuserIPs) == 0 {
		app.Settings().SuperuserIPs = []string{"127.0.0.1", "::1"}
		slog.Info("restricted superuser API access to loopback")
	}

	if cfg.PromoteAdmin != "" {
		slog.Info("promoting user to admin")
		user, err := st.PromoteUserByEmail(cfg.PromoteAdmin)
		if err != nil {
			slog.Error("promote-admin failed", "error", err)
			os.Exit(1)
		}
		slog.Info("user promoted to admin, exiting", "userId", user.ID, "email", user.Email)
		os.Exit(0)
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

	if cfg.MigrateChapterPositions {
		slog.Info("running chapter positions migration")
		if err := st.RunChapterPositionsMigration(); err != nil {
			slog.Error("chapter positions migration failed", "error", err)
			os.Exit(1)
		}
		slog.Info("chapter positions migration finished, exiting")
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
