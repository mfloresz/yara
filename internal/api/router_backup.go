package api

import (
	"archive/zip"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// backupDownload is mounted under the admin group (/api/v1/admin/backups/export)
// because the archive streams the whole data dir — every user's data plus the
// app encryption key — so it must never be reachable by a regular invited user.
// POST is correct here: the server is generating and returning a fresh
// archive every time, so the action is not safe/idempotent in the GET
// sense (the body is non-deterministic with respect to disk state).
func backupDownload(s *Server) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		dataDir := s.Cfg.DataDir

		// Checkpoint the WAL into data.db before streaming: the walker skips
		// -wal/-shm files, so without a checkpoint the copied .db would miss
		// recent writes. A failed checkpoint is non-fatal (the backup still
		// streams) but is logged.
		if _, err := s.Store.App.DB().NewQuery("PRAGMA wal_checkpoint(TRUNCATE)").Execute(); err != nil {
			slog.Warn("backup checkpoint failed, streaming anyway", "error", err)
		}

		slog.Info("backup exported", "actorId", e.Auth.Id)

		// Build the zip into a temp file OUTSIDE dataDir first (so the walker
		// can never pick up its own output). Only after the archive closes
		// cleanly do we send headers + Content-Length. The previous io.Pipe
		// streaming sent 200 before knowing whether the walk would succeed,
		// so a mid-walk failure or a cut connection produced a partial zip
		// that still looked like a successful download — always missing the
		// tail entries (storage/ sorts last).
		tmp, err := os.CreateTemp("", "backup-*.zip")
		if err != nil {
			slog.Error("backup temp file failed", "error", err)
			return e.InternalServerError("backup failed", err)
		}
		tmpName := tmp.Name()
		files, werr := writeBackupZip(tmp, dataDir)
		syncErr := tmp.Sync()
		closeErr := tmp.Close()
		if werr != nil {
			os.Remove(tmpName)
			slog.Error("backup walk failed", "error", werr)
			return e.InternalServerError("backup failed", werr)
		}
		if syncErr != nil {
			os.Remove(tmpName)
			slog.Error("backup sync failed", "error", syncErr)
			return e.InternalServerError("backup failed", syncErr)
		}
		if closeErr != nil {
			os.Remove(tmpName)
			slog.Error("backup close failed", "error", closeErr)
			return e.InternalServerError("backup failed", closeErr)
		}
		st, err := os.Stat(tmpName)
		if err != nil {
			os.Remove(tmpName)
			slog.Error("backup stat failed", "error", err)
			return e.InternalServerError("backup failed", err)
		}
		slog.Info("backup built", "actorId", e.Auth.Id, "files", files, "bytes", st.Size())

		f, err := os.Open(tmpName)
		if err != nil {
			os.Remove(tmpName)
			slog.Error("backup reopen failed", "error", err)
			return e.InternalServerError("backup failed", err)
		}
		defer func() {
			f.Close()
			os.Remove(tmpName)
		}()

		filename := "backup-" + time.Now().Format("20060102-150405") + ".zip"

		e.Response.Header().Set("Content-Type", "application/zip")
		e.Response.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")

		// ServeContent sets Content-Length (and handles Range/If-Modified-Since),
		// so a truncated transfer is detectable client-side via blob.size.
		http.ServeContent(e.Response, e.Request, filename, st.ModTime(), f)
		return nil
	}
}

func writeBackupZip(w io.Writer, dataDir string) (int, error) {
	zw := zip.NewWriter(w)
	files := 0

	err := filepath.Walk(dataDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		name := info.Name()

		// skip temp sqlite files
		if strings.HasSuffix(name, ".db-wal") || strings.HasSuffix(name, ".db-shm") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// skip directories (zip entries are implicit from file paths)
		if info.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(dataDir, path)
		if err != nil {
			return err
		}

		f, err := zw.Create(rel)
		if err != nil {
			return err
		}

		src, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(f, src)
		src.Close()
		if copyErr != nil {
			return copyErr
		}
		files++

		return nil
	})
	if err != nil {
		zw.Close()
		return files, err
	}

	if err := zw.Close(); err != nil {
		return files, err
	}
	return files, nil
}
