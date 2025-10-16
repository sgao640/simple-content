package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/google/uuid"
	"github.com/tendant/simple-content/pkg/simplecontent"
)

const DEFAULT_STORAGE_BACKEND = "s3-deafult"

// FilesHandler handles file upload and management API endpoints using pkg/simplecontent
type FilesHandler struct {
	service        simplecontent.Service
	storageService simplecontent.StorageService
}

func NewFilesHandler(service simplecontent.Service, storageService simplecontent.StorageService) *FilesHandler {
	return &FilesHandler{
		service:        service,
		storageService: storageService,
	}
}

// Routes returns the router for files endpoints
func (h *FilesHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/", h.CreateFile)
	r.Post("/{content_id}/complete", h.CompleteUpload)
	r.Get("/{content_id}", h.GetFileInfo)
	r.Get("/bulk", h.GetFilesByContentIDs)
	return r
}

// CreateFileRequest represents the request to create a new file
type CreateFileRequest struct {
	OwnerID            string `json:"owner_id"`
	OwnerType          string `json:"owner_type"`
	TenantID           string `json:"tenant_id"`
	FileName           string `json:"file_name"`
	MimeType           string `json:"mime_type,omitempty"`
	FileSize           int64  `json:"file_size,omitempty"`
	DocumentType       string `json:"document_type,omitempty"`
	StorageBackendName string `json:"storage_backend_name,omitempty"`
}

// CreateFileResponse represents the response after creating a file
type CreateFileResponse struct {
	ContentID string    `json:"content_id"`
	ObjectID  string    `json:"object_id"`
	UploadURL string    `json:"upload_url"`
	CreatedAt time.Time `json:"created_at"`
	Status    string    `json:"status"`
}

// UpdateMetadataRequest represents the request to update file metadata
type UpdateMetadataRequest struct {
	Title       string                 `json:"title,omitempty"`
	Description string                 `json:"description,omitempty"`
	Tags        []string               `json:"tags,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// FileInfoResponse represents file information including URLs
type FileInfoResponse struct {
	ContentID   string                 `json:"content_id"`
	FileName    string                 `json:"file_name"`
	PreviewURL  string                 `json:"preview_url"`
	DownloadURL string                 `json:"download_url"`
	Metadata    map[string]interface{} `json:"metadata"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
	Status      string                 `json:"status"`
	MimeType    string                 `json:"mime_type"`
	FileSize    int64                  `json:"file_size"`
	OwnerID     string                 `json:"owner_id"`
	OwnerType   string                 `json:"owner_type"`
	TenantID    string                 `json:"tenant_id"`
}

// CreateFile creates a new content and returns upload URL
func (h *FilesHandler) CreateFile(w http.ResponseWriter, r *http.Request) {
	var req CreateFileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		slog.Error("Failed to decode request", "error", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Parse UUIDs
	ownerID, err := uuid.Parse(req.OwnerID)
	if err != nil {
		slog.Error("Invalid owner ID", "owner_id", req.OwnerID, "error", err)
		http.Error(w, "Invalid owner ID", http.StatusBadRequest)
		return
	}

	if req.OwnerType == "" {
		slog.Error("Owner type is required", "owner_type", req.OwnerType)
		http.Error(w, "Owner type is required", http.StatusBadRequest)
		return
	}

	if req.DocumentType == "" {
		slog.Error("Document type is required", "document_type", req.DocumentType)
		http.Error(w, "Document type is required", http.StatusBadRequest)
		return
	}

	tenantID, err := uuid.Parse(req.TenantID)
	if err != nil {
		slog.Error("Invalid tenant ID", "tenant_id", req.TenantID, "error", err)
		http.Error(w, "Invalid tenant ID", http.StatusBadRequest)
		return
	}

	// Create Content
	content, err := h.service.CreateContent(r.Context(), simplecontent.CreateContentRequest{
		TenantID:     tenantID,
		OwnerID:      ownerID,
		OwnerType:    req.OwnerType,
		Name:         req.FileName,
		DocumentType: req.DocumentType,
	})
	if err != nil {
		slog.Error("Failed to create content", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Set Content Metadata
	metadataParams := simplecontent.SetContentMetadataRequest{
		ContentID:   content.ID,
		FileName:    req.FileName,
		ContentType: req.MimeType,
		FileSize:    req.FileSize,
	}
	if err := h.service.SetContentMetadata(r.Context(), metadataParams); err != nil {
		slog.Error("Failed to set content metadata", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	storageBackendName := req.StorageBackendName
	if storageBackendName == "" {
		storageBackendName = DEFAULT_STORAGE_BACKEND
	}
	// Create object
	object, err := h.storageService.CreateObject(r.Context(), simplecontent.CreateObjectRequest{
		ContentID:          content.ID,
		StorageBackendName: storageBackendName,
		Version:            1,
	})
	if err != nil {
		slog.Error("Failed to create object", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Set Object Metadata
	if err := h.storageService.SetObjectMetadata(r.Context(), object.ID, map[string]interface{}{
		"mime_type":  req.MimeType,
		"size_bytes": req.FileSize,
		"file_name":  req.FileName,
	}); err != nil {
		slog.Error("Failed to set object metadata", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Generate upload URL
	uploadURL, err := h.storageService.GetUploadURL(r.Context(), object.ID)
	if err != nil {
		slog.Error("Failed to generate upload URL", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := CreateFileResponse{
		ContentID: content.ID.String(),
		ObjectID:  object.ID.String(),
		UploadURL: uploadURL,
		CreatedAt: time.Now(),
		Status:    content.Status,
	}

	slog.Info("File created", "response", resp)
	render.JSON(w, r, resp)
}

// CompleteUpload marks a client-side upload as complete
func (h *FilesHandler) CompleteUpload(w http.ResponseWriter, r *http.Request) {
	contentIDStr := chi.URLParam(r, "content_id")
	contentID, err := uuid.Parse(contentIDStr)
	if err != nil {
		slog.Error("Invalid content ID", "content_id", contentIDStr, "error", err)
		http.Error(w, "Invalid content ID", http.StatusBadRequest)
		return
	}

	// Complete the upload using the unified API
	if err := h.service.UpdateContentStatus(r.Context(), contentID, simplecontent.ContentStatusUploaded); err != nil {
		slog.Error("Failed to complete upload", "content_id", contentID.String(), "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	slog.Info("Upload completed", "content_id", contentID.String())
	w.WriteHeader(http.StatusOK)
	render.JSON(w, r, map[string]string{"status": "completed"})
}

// GetFileInfo returns file information including preview and download URLs
func (h *FilesHandler) GetFileInfo(w http.ResponseWriter, r *http.Request) {
	contentIDStr := chi.URLParam(r, "content_id")
	contentID, err := uuid.Parse(contentIDStr)
	if err != nil {
		slog.Error("Invalid content ID", "content_id", contentIDStr, "error", err)
		http.Error(w, "Invalid content ID", http.StatusBadRequest)
		return
	}

	// Use GetContentDetails - the unified API
	details, err := h.service.GetContentDetails(r.Context(), contentID)
	if err != nil {
		slog.Error("Failed to get content details", "content_id", contentID.String(), "error", err)
		http.Error(w, "Content not found", http.StatusNotFound)
		return
	}

	// Get the actual content for timestamps
	content, err := h.service.GetContent(r.Context(), contentID)
	if err != nil {
		slog.Error("Failed to get content", "content_id", contentID.String(), "error", err)
		http.Error(w, "Content not found", http.StatusNotFound)
		return
	}

	resp := FileInfoResponse{
		ContentID:   details.ID,
		FileName:    details.FileName,
		PreviewURL:  details.Preview,
		DownloadURL: details.Download,
		CreatedAt:   details.CreatedAt,
		UpdatedAt:   details.UpdatedAt,
		MimeType:    details.MimeType,
		FileSize:    details.FileSize,
		OwnerID:     content.OwnerID.String(),
		OwnerType:   content.OwnerType,
		TenantID:    content.TenantID.String(),
		Status:      content.Status,
	}

	slog.Info("GetFileInfo", "content_id", contentID.String())
	render.JSON(w, r, resp)
}

// GetFilesByContentIDs retrieves multiple files by their IDs
func (h *FilesHandler) GetFilesByContentIDs(w http.ResponseWriter, r *http.Request) {
	// Get the id parameters from the query string
	idStrings := r.URL.Query()["id"]
	if len(idStrings) == 0 {
		http.Error(w, "Missing required 'id' parameter", http.StatusBadRequest)
		return
	}

	const maxContentsPerRequest = 50
	if len(idStrings) > maxContentsPerRequest {
		http.Error(w, "Too many IDs requested", http.StatusBadRequest)
		return
	}

	// Create a slice to hold the response
	var files []FileInfoResponse

	// Process each ID
	for _, idStr := range idStrings {
		// Parse the UUID
		id, err := uuid.Parse(idStr)
		if err != nil {
			slog.Warn("Invalid content ID", "id", idStr)
			continue
		}

		// Get content details
		details, err := h.service.GetContentDetails(r.Context(), id)
		if err != nil {
			slog.Warn("Failed to get content details", "id", idStr, "error", err)
			continue
		}

		// Get the actual content for timestamps
		content, err := h.service.GetContent(r.Context(), id)
		if err != nil {
			slog.Warn("Failed to get content", "id", idStr, "error", err)
			continue
		}

		resp := FileInfoResponse{
			ContentID:   details.ID,
			FileName:    details.FileName,
			PreviewURL:  details.Preview,
			DownloadURL: details.Download,
			CreatedAt:   content.CreatedAt,
			UpdatedAt:   content.UpdatedAt,
			Status:      content.Status,
			MimeType:    details.MimeType,
			FileSize:    details.FileSize,
			OwnerID:     content.OwnerID.String(),
			OwnerType:   content.OwnerType,
			TenantID:    content.TenantID.String(),
		}

		files = append(files, resp)
	}

	render.JSON(w, r, files)
}
