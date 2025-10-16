package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/ilyakaznacheev/cleanenv"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tendant/chi-demo/app"
	"github.com/tendant/chi-demo/middleware"
	"github.com/tendant/simple-content/pkg/simplecontent"
	scapi "github.com/tendant/simple-content/pkg/simplecontent/api"
	"github.com/tendant/simple-content/pkg/simplecontent/objectkey"
	repopg "github.com/tendant/simple-content/pkg/simplecontent/repo/postgres"
	s3storage "github.com/tendant/simple-content/pkg/simplecontent/storage/s3"
	"github.com/tendant/simple-content/pkg/simplecontent/urlstrategy"
)

type Config struct {
	DB           DbConfig
	S3           S3Config
	ApiKeySHA256 string `env:"API_KEY_SHA256"`
	NoticeConfig NoticeConfig
}

type NoticeConfig struct {
	EventAuditUrl string `env:"EVENT_AUDIT_URL" env-default:"http://localhost:14000/events"`
}

type DbConfig struct {
	Port     uint16 `env:"CONTENT_PG_PORT" env-default:"5432"`
	Host     string `env:"CONTENT_PG_HOST" env-default:"localhost"`
	Name     string `env:"CONTENT_PG_NAME" env-default:"powercard_db"`
	User     string `env:"CONTENT_PG_USER" env-default:"content"`
	Password string `env:"CONTENT_PG_PASSWORD" env-default:"pwd"`
}

type S3Config struct {
	Endpoint        string `env:"AWS_S3_ENDPOINT"`
	AccessKeyID     string `env:"AWS_ACCESS_KEY_ID" env-default:"minioadmin"`
	SecretAccessKey string `env:"AWS_SECRET_ACCESS_KEY" env-default:"minioadmin"`
	BucketName      string `env:"AWS_S3_BUCKET" env-default:"content-bucket"`
	Region          string `env:"AWS_S3_REGION" env-default:"us-east-1"`
	UseSSL          bool   `env:"AWS_S3_USE_SSL" env-default:"false"`
}

const S3_URL_DURATION = 3600 * 6 // 6 hours
const DEFAULT_STORAGE_BACKEND = "s3-default"

func (c DbConfig) toDatabaseUrl() string {
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(c.User, c.Password),
		Host:   fmt.Sprintf("%s:%d", c.Host, c.Port),
		Path:   c.Name,
	}
	return u.String()
}

func NewDbPool(ctx context.Context, dbConfig DbConfig) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, dbConfig.toDatabaseUrl())
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	// Test the connection
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return pool, nil
}

func initializeS3Backend(config S3Config) (simplecontent.BlobStore, error) {
	s3Config := s3storage.Config{
		Endpoint:               config.Endpoint,
		AccessKeyID:            config.AccessKeyID,
		SecretAccessKey:        config.SecretAccessKey,
		Bucket:                 config.BucketName,
		Region:                 config.Region,
		UseSSL:                 config.UseSSL,
		CreateBucketIfNotExist: false,
		PresignDuration:        S3_URL_DURATION, // 6 hours
	}

	backend, err := s3storage.New(s3Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create S3 backend: %w", err)
	}

	return backend, nil
}

func main() {
	// Load configuration
	var config Config
	if err := cleanenv.ReadEnv(&config); err != nil {
		slog.Error("Failed to read configuration", "err", err)
		os.Exit(1)
	}
	apiKeyConfig := middleware.ApiKeyConfig{
		APIKeys: map[string]string{
			"key1": config.ApiKeySHA256,
		},
	}

	// Initialize database connection
	ctx := context.Background()
	dbPool, err := NewDbPool(ctx, config.DB)
	if err != nil {
		slog.Error("Failed to connect to database", "err", err)
		os.Exit(1)
	}

	// Initialize repository
	repo := repopg.NewWithPool(dbPool)

	// Initialize S3 storage backend
	s3Backend, err := initializeS3Backend(config.S3)
	if err != nil {
		slog.Error("Failed to initialize S3 backend", "err", err)
		os.Exit(1)
	}

	// Initialize URL strategy - using storage-delegated strategy for S3 presigned URLs
	blobStores := map[string]urlstrategy.BlobStore{
		DEFAULT_STORAGE_BACKEND: s3Backend,
	}
	urlStrategy := urlstrategy.NewStorageDelegatedStrategy(blobStores)

	// Initialize object key generator - using git-like for better performance
	keyGenerator := objectkey.NewGitLikeGenerator()

	// Initialize service with new pkg/simplecontent API
	// Note: The service will automatically use the first registered backend as default
	service, err := simplecontent.New(
		simplecontent.WithRepository(repo),
		simplecontent.WithBlobStore(DEFAULT_STORAGE_BACKEND, s3Backend),
		simplecontent.WithURLStrategy(urlStrategy),
		simplecontent.WithObjectKeyGenerator(keyGenerator),
		simplecontent.WithEventSink(simplecontent.NewNoopEventSink()),
		simplecontent.WithPreviewer(simplecontent.NewBasicImagePreviewer()),
	)
	if err != nil {
		slog.Error("Failed to initialize service", "err", err)
		os.Exit(1)
	}

	// Initialize storage service for advanced object operations
	storageService, err := simplecontent.NewStorageService(
		simplecontent.WithRepository(repo),
		simplecontent.WithBlobStore(DEFAULT_STORAGE_BACKEND, s3Backend),
		simplecontent.WithURLStrategy(urlStrategy),
		simplecontent.WithObjectKeyGenerator(keyGenerator),
		simplecontent.WithEventSink(simplecontent.NewNoopEventSink()),
		simplecontent.WithPreviewer(simplecontent.NewBasicImagePreviewer()),
	)
	if err != nil {
		slog.Error("Failed to initialize storage service", "err", err)
		os.Exit(1)
	}

	server := app.DefaultApp()

	app.RoutesHealthz(server.R)
	app.RoutesHealthzReady(server.R)

	// Initialize API handlers with new pkg/simplecontent/api
	contentHandler := scapi.NewContentHandler(service, storageService)
	filesHandler := scapi.NewFilesHandler(service, storageService)

	apiKeyMiddleware, err := middleware.ApiKeyMiddleware(apiKeyConfig)
	if err != nil {
		slog.Error("Failed initialize API Key middleware", "err", err)
		return
	}
	server.R.Route("/api/v5", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(apiKeyMiddleware)
			r.Mount("/files", filesHandler.Routes())
			r.Mount("/contents", contentHandler.Routes())
		})
	})

	defer dbPool.Close()

	// Start server
	server.Run()
}

func RoutesHealthz(r *chi.Mux) {
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		render.PlainText(w, r, http.StatusText(http.StatusOK))
	})
}

func RoutesHealthzReady(r *chi.Mux) {
	r.Get("/healthz/ready", func(w http.ResponseWriter, r *http.Request) {
		render.PlainText(w, r, http.StatusText(http.StatusOK))
	})
}
