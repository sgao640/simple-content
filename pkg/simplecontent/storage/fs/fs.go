package fs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/tendant/simple-content/pkg/simplecontent"
	"github.com/tendant/simple-content/pkg/simplecontent/presigned"
)

// Backend is a filesystem implementation of the simplecontent.BlobStore interface
type Backend struct {
	mu             sync.RWMutex
	baseDir        string
	urlPrefix      string
	signer         *presigned.Signer // For authenticated presigned URLs
	presignExpires time.Duration     // Default expiration for presigned URLs
}

// Config options for the filesystem backend
type Config struct {
	BaseDir            string        // Base directory for storing files
	URLPrefix          string        // Optional URL prefix for download/upload URLs
	SignatureSecretKey string        // Secret key for signing presigned URLs (optional, enables auth)
	PresignExpires     time.Duration // Default expiration for presigned URLs (default: 1 hour)
}

// New creates a new filesystem storage backend
func New(config Config) (simplecontent.BlobStore, error) {
	// Validate and create base directory if it doesn't exist
	if config.BaseDir == "" {
		return nil, errors.New("base directory is required")
	}

	if err := os.MkdirAll(config.BaseDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create base directory: %w", err)
	}

	// Set default presign expiration
	presignExpires := config.PresignExpires
	if presignExpires == 0 {
		presignExpires = 1 * time.Hour // Default: 1 hour
	}

	backend := &Backend{
		baseDir:        config.BaseDir,
		urlPrefix:      config.URLPrefix,
		presignExpires: presignExpires,
	}

	// Initialize presigned signer if secret key is provided
	if config.SignatureSecretKey != "" {
		backend.signer = presigned.New(
			presigned.WithSecretKey(config.SignatureSecretKey),
			presigned.WithDefaultExpiration(presignExpires),
			presigned.WithURLPattern("/upload/{key}"),
		)
	}

	return backend, nil
}

// GetObjectMeta retrieves metadata for an object in the filesystem
func (b *Backend) GetObjectMeta(ctx context.Context, objectKey string) (*simplecontent.ObjectMeta, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	filePath := filepath.Join(b.baseDir, objectKey)

	// Check if file exists
	info, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		return nil, errors.New("object not found")
	} else if err != nil {
		return nil, fmt.Errorf("failed to get file info: %w", err)
	}

	// Detect content type
	contentType := "application/octet-stream"
	if file, err := os.Open(filePath); err == nil {
		defer file.Close()
		buffer := make([]byte, 512)
		if n, err := file.Read(buffer); err == nil {
			contentType = http.DetectContentType(buffer[:n])
		}
	}

	meta := &simplecontent.ObjectMeta{
		Key:         objectKey,
		Size:        info.Size(),
		ContentType: contentType,
		UpdatedAt:   info.ModTime(),
		Metadata:    map[string]string{"content_type": contentType},
	}

	return meta, nil
}

// GetUploadURL returns a URL for uploading content
// When urlPrefix is configured, returns a URL that can be used for presigned-style uploads
// This allows testing presigned upload workflows locally with filesystem storage
// If SignatureSecretKey is configured, the URL will be signed with HMAC for security
func (b *Backend) GetUploadURL(ctx context.Context, objectKey string) (string, error) {
	if b.urlPrefix == "" {
		return "", errors.New("direct upload required for filesystem backend")
	}

	path := "/upload/" + objectKey

	// If signer is configured, generate signed URL
	if b.signer != nil {
		return b.signer.SignURLWithBase(b.urlPrefix, "PUT", path, b.presignExpires)
	}

	// Otherwise, return unsigned URL (for backward compatibility)
	return b.urlPrefix + path, nil
}

// Upload uploads content directly to the filesystem
func (b *Backend) Upload(ctx context.Context, objectKey string, reader io.Reader) error {
	filePath := filepath.Join(b.baseDir, objectKey)

	// Create directory structure if it doesn't exist
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Create file
	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	// Copy data from reader to file
	_, err = io.Copy(file, reader)
	if err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// UploadWithParams uploads content with additional parameters
func (b *Backend) UploadWithParams(ctx context.Context, reader io.Reader, params simplecontent.UploadParams) error {
	// For filesystem, we don't store MIME type separately, it's detected on read
	return b.Upload(ctx, params.ObjectKey, reader)
}

// GetDownloadURL returns a URL for downloading content
func (b *Backend) GetDownloadURL(ctx context.Context, objectKey string, downloadFilename string) (string, error) {
	if b.urlPrefix == "" {
		return "", errors.New("direct download required for filesystem backend")
	}

	// Include the download filename in the URL if provided
	if downloadFilename != "" {
		return fmt.Sprintf("%s/download/%s?filename=%s", b.urlPrefix, objectKey, downloadFilename), nil
	}
	return fmt.Sprintf("%s/download/%s", b.urlPrefix, objectKey), nil
}

// GetPreviewURL returns a URL for previewing content
func (b *Backend) GetPreviewURL(ctx context.Context, objectKey string) (string, error) {
	if b.urlPrefix == "" {
		return "", errors.New("direct preview required for filesystem backend")
	}
	return fmt.Sprintf("%s/preview/%s", b.urlPrefix, objectKey), nil
}

// Download downloads content directly from the filesystem
func (b *Backend) Download(ctx context.Context, objectKey string) (io.ReadCloser, error) {
	filePath := filepath.Join(b.baseDir, objectKey)

	// Check if file exists and open it
	file, err := os.Open(filePath)
	if os.IsNotExist(err) {
		return nil, errors.New("object not found")
	} else if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}

	return file, nil
}

// Delete deletes content from the filesystem
func (b *Backend) Delete(ctx context.Context, objectKey string) error {
	filePath := filepath.Join(b.baseDir, objectKey)

	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return errors.New("object not found")
	}

	// Delete file
	if err := os.Remove(filePath); err != nil {
		return fmt.Errorf("failed to delete file: %w", err)
	}

	// Clean up empty directories
	b.cleanupEmptyDirectories(filepath.Dir(filePath))

	return nil
}

// cleanupEmptyDirectories recursively removes empty directories up to baseDir
func (b *Backend) cleanupEmptyDirectories(dir string) {
	// Don't remove the base directory
	if dir == b.baseDir {
		return
	}

	// Check if directory is empty
	if entries, err := os.ReadDir(dir); err == nil && len(entries) == 0 {
		// Remove empty directory
		if os.Remove(dir) == nil {
			// Recursively clean parent directory
			b.cleanupEmptyDirectories(filepath.Dir(dir))
		}
	}
}

// ValidateUploadSignature validates a presigned upload URL signature
// Returns nil if signature is valid, error otherwise
func (b *Backend) ValidateUploadSignature(objectKey, signature string, expiresAt int64) error {
	if b.signer == nil {
		// No signature validation configured - allow all uploads
		// This provides backward compatibility
		return nil
	}

	path := "/upload/" + objectKey
	return b.signer.Validate("PUT", path, signature, expiresAt)
}

// IsSignedURLEnabled returns true if signed URLs are enabled for this backend
func (b *Backend) IsSignedURLEnabled() bool {
	return b.signer != nil && b.signer.IsEnabled()
}

// GetSigner returns the presigned.Signer if configured, nil otherwise
// This allows external code to use the signer directly
func (b *Backend) GetSigner() *presigned.Signer {
	return b.signer
}
