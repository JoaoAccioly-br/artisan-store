package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joao-accioly/artisan-store/api/internal/config"
	apphttp "github.com/joao-accioly/artisan-store/api/internal/http"
	"github.com/joao-accioly/artisan-store/api/internal/platform/db"
	"github.com/joao-accioly/artisan-store/api/internal/platform/logger"
	"github.com/joao-accioly/artisan-store/api/internal/platform/storage"
	"github.com/joao-accioly/artisan-store/api/internal/security"
	"github.com/joao-accioly/artisan-store/api/internal/service"
)

func main() {
	if err := run(); err != nil {
		slog.Error("application terminated with error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	log := logger.New(cfg.AppEnv, cfg.LogLevel)
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	if cfg.AutoMigrate {
		if migErr := db.MigrateUp(ctx, pool); migErr != nil {
			return migErr
		}
		log.Info("database migrations applied successfully")
	}

	hasher := security.NewArgon2idHasher(security.DefaultArgon2idParams())

	if seedErr := db.SeedAdmin(ctx, pool, cfg, hasher, log); seedErr != nil {
		return seedErr
	}

	jwtManager, err := security.NewJWTManager(cfg.JWTAccessSecret, cfg.JWTAccessTTL)
	if err != nil {
		return err
	}

	imgStorage, uploadHandler, err := initStorage(ctx, cfg)
	if err != nil {
		return err
	}

	authService, err := service.NewAuthService(pool, hasher, jwtManager, cfg.JWTRefreshTTL, log)
	if err != nil {
		return err
	}
	userService := service.NewUserService(pool, hasher)
	catalogService, err := service.NewCatalogService(pool, imgStorage, cfg.StorageMaxUploadBytes, log)
	if err != nil {
		return err
	}

	router := apphttp.NewRouter(cfg, log, apphttp.RouterDeps{
		DB:             pool,
		JWTManager:     jwtManager,
		Auth:           authService,
		Users:          userService,
		Catalog:        catalogService,
		MaxUploadBytes: cfg.StorageMaxUploadBytes,
		UploadHandler:  uploadHandler,
	})

	srv := &http.Server{
		Addr:              cfg.Address(),
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("http server listening",
			slog.String("addr", srv.Addr),
			slog.String("env", cfg.AppEnv),
			slog.String("storage_driver", cfg.StorageDriver),
		)
		if serveErr := srv.ListenAndServe(); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			errCh <- serveErr
		}
		close(errCh)
	}()

	select {
	case serveErr := <-errCh:
		return serveErr
	case <-ctx.Done():
		log.Info("shutdown signal received, draining connections...")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}

	log.Info("http server stopped gracefully")
	return nil
}

func initStorage(ctx context.Context, cfg *config.Config) (storage.Storage, http.Handler, error) {
	switch cfg.StorageDriver {
	case config.StorageDriverLocal:
		localStore, err := storage.NewLocalStorage(cfg.StorageLocalDir, cfg.StoragePublicURL)
		if err != nil {
			return nil, nil, fmt.Errorf("init local storage: %w", err)
		}
		return localStore, localStore.FileServerHandler("/uploads"), nil
	case config.StorageDriverS3:
		s3Store, err := storage.NewS3Storage(ctx, storage.S3Config{
			Endpoint:      cfg.StorageS3Endpoint,
			Region:        cfg.StorageS3Region,
			Bucket:        cfg.StorageS3Bucket,
			AccessKey:     cfg.StorageS3AccessKey,
			SecretKey:     cfg.StorageS3SecretKey,
			PublicBaseURL: cfg.StoragePublicURL,
			UsePathStyle:  cfg.StorageS3UsePathStyle,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("init s3 storage: %w", err)
		}
		return s3Store, nil, nil
	default:
		return nil, nil, fmt.Errorf("unsupported storage driver %q", cfg.StorageDriver)
	}
}
