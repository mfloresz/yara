package api

import (
	"archive/zip"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
	pbrouter "github.com/pocketbase/pocketbase/tools/router"
)

func registerBackupRoutes(api *pbrouter.RouterGroup[*core.RequestEvent], s *Server) {
	api.GET("/backup/download", func(e *core.RequestEvent) error {
		dataDir := s.Cfg.DataDir

		pr, pw := io.Pipe()

		go func() {
			err := writeBackupZip(pw, dataDir)
			pw.CloseWithError(err)
		}()

		filename := "backup-" + time.Now().Format("20060102-150405") + ".zip"

		e.Response.Header().Set("Content-Type", "application/zip")
		e.Response.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")

		if _, err := io.Copy(e.Response, pr); err != nil {
			slog.Error("backup stream failed", "error", err)
			return e.InternalServerError("backup stream failed", err)
		}

		return nil
	})
}

func writeBackupZip(pw *io.PipeWriter, dataDir string) error {
	w := zip.NewWriter(pw)
	defer w.Close()

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

		f, err := w.Create(rel)
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

		return nil
	})
	if err != nil {
		slog.Error("backup walk failed", "error", err)
		return err
	}

	return w.Close()
}
