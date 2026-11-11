// Command server runs the filedrop HTTP server.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"filedrop/internal/api"
	"filedrop/internal/blob"
	"filedrop/internal/config"
	"filedrop/internal/store"
)

// version is the release version; override with -ldflags "-X main.version=...".
var version = "1.0.0"

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "-version" || os.Args[1] == "--version") {
		fmt.Println("filedrop", version)
		return
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.DataDir, 0o750); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, filepath.Join(cfg.DataDir, "filedrop.db"))
	if err != nil {
		return err
	}
	defer st.Close()

	blobs, err := blob.New(filepath.Join(cfg.DataDir, "blobs"))
	if err != nil {
		return err
	}

	app := api.New(cfg, st, blobs, log)
	srv := &http.Server{
		Addr:    cfg.Addr,
		Handler: app.Handler(),
		// No ReadTimeout/WriteTimeout: they would cut large uploads and downloads.
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	defer signal.Stop(hup)

	errc := make(chan error, 1)
	go func() {
		log.Info("listening", "version", version, "addr", cfg.Addr, "data_dir", cfg.DataDir, "max_upload_size", cfg.MaxUploadSize)
		errc <- srv.ListenAndServe()
	}()

wait:
	for {
		select {
		case err := <-errc:
			return err
		case <-hup:
			cfg = reload(log, app, cfg)
		case <-ctx.Done():
			break wait
		}
	}

	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// reload re-reads the configuration and applies it to app. On error, or for
// settings that need a restart (ADDR, DATA_DIR), the current values are kept.
// It returns the configuration now in effect.
func reload(log *slog.Logger, app *api.Server, cur config.Config) config.Config {
	cfg, err := config.Load()
	if err != nil {
		log.Error("reload failed, keeping the current configuration", "err", err)
		return cur
	}
	if cfg.Addr != cur.Addr {
		log.Warn("ADDR changed, restart to apply it", "addr", cur.Addr, "new_addr", cfg.Addr)
		cfg.Addr = cur.Addr
	}
	if cfg.DataDir != cur.DataDir {
		log.Warn("DATA_DIR changed, restart to apply it", "data_dir", cur.DataDir, "new_data_dir", cfg.DataDir)
		cfg.DataDir = cur.DataDir
	}
	app.SetConfig(cfg)
	log.Info("configuration reloaded", "base_url", cfg.BaseURL, "max_upload_size", cfg.MaxUploadSize, "slug_length", cfg.SlugLength)
	return cfg
}
