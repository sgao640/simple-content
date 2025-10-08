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

// ObjectResponse is the response body for an object
type ObjectResponse struct {
	ID                 string    `json:"id"`
	ContentID          string    `json:"content_id"`
	StorageBackendName string    `json:"storage_backend_name"`
	Version            int       `json:"version"`
	ObjectKey          string    `json:"object_key"`
	Status             string    `json:"status"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
	UploadURL          string    `json:"upload_url"`
}

// CreateObjectRequest is the request body for creating an object
type CreateObjectRequest struct {
	StorageBackendName string `json:"storage_backend_name"`
	Version            int    `json:"version"`
	ObjectKey          string `json:"object_key"`
	MimeType           string `json:"mime_type"`
	FileSize           int64  `json:"file_size"`
	FileName           string `json:"file_name"`
}

// ContentHandler handles HTTP requests for content using pkg/simplecontent
type ContentHandler struct {
	service simplecontent.Service
	storage simplecontent.StorageService
}

// NewContentHandler creates a new content handler
func NewContentHandler(service simplecontent.Service, storageService simplecontent.StorageService) *ContentHandler {
	return &ContentHandler{
		service: service,
		storage: storageService,
	}
}

// Routes returns the routes for content
func (h *ContentHandler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Post("/", h.CreateContent)
	r.Get("/{id}", h.GetContent)
	r.Delete("/{id}", h.DeleteContent)
	r.Get("/bulk", h.GetContentsByIDs)

	r.Post("/{id}/objects", h.CreateObject)
	r.Get("/{id}/objects", h.ListObjects)

	// Routes for derived content
	r.Post("/{id}/derived", h.CreateDerivedContent)

	return r
}

// CreateContentRequest is the request body for creating a content
type CreateContentRequest struct {
	OwnerID      string `json:"owner_id"`
	TenantID     string `json:"tenant_id"`
	DocumentType string `json:"document_type"`
	FileName     string `json:"file_name"`
	OwnerType    string `json:"owner_type"`
	MimeType     string `json:"mime_type"`
	FileSize     int64  `json:"file_size"`
}

// ContentResponse is the response body for a content
type ContentResponse struct {
	ID           string    `json:"id"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	OwnerID      string    `json:"owner_id"`
	TenantID     string    `json:"tenant_id"`
	Status       string    `json:"status"`
	MimeType     string    `json:"mime_type"`
	FileSize     int64     `json:"file_size"`
	FileName     string    `json:"file_name"`
	DocumentType string    `json:"document_type"`
}

const maxContentsPerRequest = 50

// CreateContent creates a new content
func (h *ContentHandler) CreateContent(w http.ResponseWriter, r *http.Request) {
	var req CreateContentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ownerID, err := uuid.Parse(req.OwnerID)
	if err != nil {
		slog.Error("Invalid owner ID", "owner_id", req.OwnerID, "error", err)
		http.Error(w, "Invalid owner ID", http.StatusBadRequest)
		return
	}

	tenantID, err := uuid.Parse(req.TenantID)
	if err != nil {
		slog.Error("Invalid tenant ID", "tenant_id", req.TenantID, "error", err)
		http.Error(w, "Invalid tenant ID", http.StatusBadRequest)
		return
	}

	// Create content using the simplified API
	createReq := simplecontent.CreateContentRequest{
		TenantID:     tenantID,
		OwnerID:      ownerID,
		OwnerType:    req.OwnerType,
		Name:         req.FileName,
		DocumentType: req.DocumentType,
	}

	content, err := h.service.CreateContent(r.Context(), createReq)
	if err != nil {
		slog.Error("Failed to create content", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Set content metadata
	if err := h.service.SetContentMetadata(r.Context(), simplecontent.SetContentMetadataRequest{
		ContentID:   content.ID,
		ContentType: req.MimeType,
		FileName:    req.FileName,
		FileSize:    req.FileSize,
		CreatedBy:   ownerID.String(),
	}); err != nil {
		slog.Error("Failed to set content metadata", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := ContentResponse{
		ID:           content.ID.String(),
		CreatedAt:    content.CreatedAt,
		UpdatedAt:    content.UpdatedAt,
		OwnerID:      content.OwnerID.String(),
		TenantID:     content.TenantID.String(),
		Status:       content.Status,
		DocumentType: content.DocumentType,
		FileName:     req.FileName,
		MimeType:     req.MimeType,
	}

	slog.Info("Content created", "content_id", content.ID.String())
	render.Status(r, http.StatusCreated)
	render.JSON(w, r, resp)
}

// GetContent retrieves a content by ID
func (h *ContentHandler) GetContent(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		slog.Error("Invalid content ID", "content_id", idStr, "error", err)
		http.Error(w, "Invalid content ID", http.StatusBadRequest)
		return
	}

	// Get Content
	content, err := h.service.GetContent(r.Context(), id)
	if err != nil {
		slog.Error("Failed to get content", "content_id", idStr, "error", err)
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	resp := ContentResponse{
		ID:           content.ID.String(),
		CreatedAt:    content.CreatedAt,
		UpdatedAt:    content.UpdatedAt,
		OwnerID:      content.OwnerID.String(),
		TenantID:     content.TenantID.String(),
		Status:       content.Status,
		DocumentType: content.DocumentType,
	}

	// Get content metadata
	contentMeta, err := h.service.GetContentMetadata(r.Context(), id)
	if err != nil {
		slog.Warn("Fail to get content metadata", "id", idStr)
	} else {
		resp.MimeType = contentMeta.MimeType
		resp.FileSize = contentMeta.FileSize
		resp.FileName = contentMeta.FileName
	}

	slog.Info("Content retrieved", "content_id", idStr)
	render.JSON(w, r, resp)
}

// GetContentsByIDs retrieves multiple contents by their IDs
func (h *ContentHandler) GetContentsByIDs(w http.ResponseWriter, r *http.Request) {
	// Get the id parameters from the query string
	idStrings := r.URL.Query()["id"]
	if len(idStrings) == 0 {
		http.Error(w, "Missing required 'id' parameter", http.StatusBadRequest)
		return
	}
	if len(idStrings) > maxContentsPerRequest {
		http.Error(w, "Too many IDs requested", http.StatusBadRequest)
		return
	}

	// Create a slice to hold the response
	var contents []ContentResponse

	// Process each ID
	for _, idStr := range idStrings {
		// Parse the UUID
		id, err := uuid.Parse(idStr)
		if err != nil {
			slog.Warn("Invalid content ID", "id", idStr)
			continue
		}

		// Get the content
		content, err := h.service.GetContent(r.Context(), id)
		if err != nil {
			slog.Warn("Failed to get content", "id", idStr)
			continue
		}

		// Create response for this content
		resp := ContentResponse{
			ID:           content.ID.String(),
			CreatedAt:    content.CreatedAt,
			UpdatedAt:    content.UpdatedAt,
			OwnerID:      content.OwnerID.String(),
			TenantID:     content.TenantID.String(),
			Status:       content.Status,
			DocumentType: content.DocumentType,
		}

		// Get content metadata
		contentMeta, err := h.service.GetContentMetadata(r.Context(), id)
		if err != nil {
			slog.Warn("Fail to get content metadata", "id", idStr)
		} else {
			resp.MimeType = contentMeta.MimeType
			resp.FileSize = contentMeta.FileSize
			resp.FileName = contentMeta.FileName
		}

		contents = append(contents, resp)
	}

	// Return all found contents
	render.JSON(w, r, contents)
}

// DeleteContent deletes a content by ID
func (h *ContentHandler) DeleteContent(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		slog.Error("Invalid content ID", "content_id", idStr, "error", err)
		http.Error(w, "Invalid content ID", http.StatusBadRequest)
		return
	}

	if err := h.service.DeleteContent(r.Context(), id); err != nil {
		slog.Error("Failed to delete content", "content_id", idStr, "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	slog.Info("Content deleted", "content_id", idStr)
	w.WriteHeader(http.StatusNoContent)
}

// GetContentDetails retrieves detailed content information with URLs
func (h *ContentHandler) GetContentDetails(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		slog.Error("Invalid content ID", "content_id", idStr, "error", err)
		http.Error(w, "Invalid content ID", http.StatusBadRequest)
		return
	}

	details, err := h.service.GetContentDetails(r.Context(), id)

	if err != nil {
		slog.Error("Failed to get content details", "content_id", idStr, "error", err)
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	slog.Info("Content details retrieved", "content_id", idStr)
	render.JSON(w, r, details)
}

// CreateObject creates a new object for a content
func (h *ContentHandler) CreateObject(w http.ResponseWriter, r *http.Request) {
	contentIDStr := chi.URLParam(r, "id")
	contentID, err := uuid.Parse(contentIDStr)
	if err != nil {
		slog.Error("Invalid content ID", "content_id", contentIDStr, "error", err)
		http.Error(w, "Invalid content ID", http.StatusBadRequest)
		return
	}

	var req CreateObjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		slog.Error("Invalid request body", "error", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Create object
	object, err := h.storage.CreateObject(r.Context(), simplecontent.CreateObjectRequest{
		ContentID:          contentID,
		StorageBackendName: "s3-default",
		Version:            1,
		//FileName:           req.FileName,
	})
	if err != nil {
		slog.Error("Fail to create object", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Set object metadata
	err = h.storage.SetObjectMetadata(r.Context(), object.ID, map[string]interface{}{
		"mime_type":  req.MimeType,
		"size_bytes": req.FileSize,
		"file_name":  req.FileName,
	})
	if err != nil {
		slog.Warn("Failed to update object metadata", "err", err)
	}

	// Get upload URL
	uploadURL, err := h.storage.GetUploadURL(r.Context(), object.ID)
	if err != nil {
		slog.Error("Failed to get upload URL", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := ObjectResponse{
		ID:                 object.ID.String(),
		ContentID:          object.ContentID.String(),
		StorageBackendName: object.StorageBackendName,
		Version:            object.Version,
		ObjectKey:          object.ObjectKey,
		Status:             object.Status,
		CreatedAt:          object.CreatedAt,
		UpdatedAt:          object.UpdatedAt,
		UploadURL:          uploadURL,
	}

	slog.Info("Object created", "object_id", resp.ID)

	render.Status(r, http.StatusCreated)
	render.JSON(w, r, resp)
}

// ListObjects lists objects for a content
// Query parameters:
//   - latest=true: Only return the latest version object (default: true)
func (h *ContentHandler) ListObjects(w http.ResponseWriter, r *http.Request) {
	contentIDStr := chi.URLParam(r, "id")
	contentID, err := uuid.Parse(contentIDStr)
	if err != nil {
		http.Error(w, "Invalid content ID", http.StatusBadRequest)
		return
	}

	objects, err := h.service.GetObjectsByContentID(r.Context(), contentID)
	if err != nil {
		slog.Error("Fail to get objects by content ID", "content_id", contentIDStr, "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if len(objects) == 0 {
		slog.Warn("No objects found for content", "content_id", contentIDStr)
		http.Error(w, "No objects found for content "+contentIDStr, http.StatusNotFound)
		return
	}
	// Check if we should only return the latest version
	latestOnly := true // Default to true for backward compatibility
	latestParam := r.URL.Query().Get("latest")
	if latestParam == "false" {
		latestOnly = false
	}

	// Filter objects based on the latest parameter
	if latestOnly {
		objects = []*simplecontent.Object{GetLatestVersionObject(objects)}
	}

	var resp []ObjectResponse
	for _, object := range objects {
		resp = append(resp, ObjectResponse{
			ID:                 object.ID.String(),
			ContentID:          object.ContentID.String(),
			StorageBackendName: object.StorageBackendName,
			Version:            object.Version,
			ObjectKey:          object.ObjectKey,
			Status:             object.Status,
			CreatedAt:          object.CreatedAt,
			UpdatedAt:          object.UpdatedAt,
		})
	}

	render.JSON(w, r, resp)
}

// GetLatestVersionObject returns the object with the highest version number from a slice of objects.
// If there are multiple objects with the same highest version, it returns the first one found.
func GetLatestVersionObject(objects []*simplecontent.Object) *simplecontent.Object {
	if len(objects) == 0 {
		return nil
	}

	latestObject := objects[0]
	for _, obj := range objects[1:] {
		if obj.Version > latestObject.Version {
			latestObject = obj
		}
	}

	return latestObject
}

// CreateDerivedContentRequest is the request body for creating derived content
type CreateDerivedContentRequest struct {
	DerivedContentID   uuid.UUID              `json:"derived_content_id"`
	DerivationType     string                 `json:"derivation_type"`
	DerivationParams   map[string]interface{} `json:"derivation_params"`
	ProcessingMetadata map[string]interface{} `json:"processing_metadata"`
	OwnerID            string                 `json:"owner_id"`
	TenantID           string                 `json:"tenant_id"`
}

// CreateDerivedContentResponse is the response body for derived content creation
type CreateDerivedContentResponse struct {
	ParentContentID  string `json:"parent_content_id"`
	DerivedContentID string `json:"derived_content_id"`
	DerivationType   string `json:"derivation_type"`
}

// CreateDerivedContent creates a new derived content from a parent content
func (h *ContentHandler) CreateDerivedContent(w http.ResponseWriter, r *http.Request) {
	parentIDStr := chi.URLParam(r, "id")
	parentID, err := uuid.Parse(parentIDStr)
	if err != nil {
		http.Error(w, "Invalid parent content ID", http.StatusBadRequest)
		return
	}

	var req CreateDerivedContentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Parse owner and tenant IDs
	ownerID, err := uuid.Parse(req.OwnerID)
	if err != nil {
		slog.Error("Invalid owner ID", "owner_id", req.OwnerID, "error", err)
		http.Error(w, "Invalid owner ID", http.StatusBadRequest)
		return
	}

	tenantID, err := uuid.Parse(req.TenantID)
	if err != nil {
		slog.Error("Invalid tenant ID", "tenant_id", req.TenantID, "error", err)
		http.Error(w, "Invalid tenant ID", http.StatusBadRequest)
		return
	}

	// Merge derivation params and processing metadata into a single metadata map
	metadata := make(map[string]interface{})
	for k, v := range req.DerivationParams {
		metadata[k] = v
	}
	for k, v := range req.ProcessingMetadata {
		metadata[k] = v
	}

	// Create derived content using the new API
	derivedContent, err := h.service.CreateDerivedContent(r.Context(), simplecontent.CreateDerivedContentRequest{
		ParentID:       parentID,
		OwnerID:        ownerID,
		TenantID:       tenantID,
		DerivationType: req.DerivationType,
		Metadata:       metadata,
	})
	if err != nil {
		slog.Error("Failed to create derived content", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := CreateDerivedContentResponse{
		ParentContentID:  parentIDStr,
		DerivedContentID: derivedContent.ID.String(),
		DerivationType:   req.DerivationType,
	}

	slog.Info("Derived content created", "parent_id", parentIDStr, "derived_id", derivedContent.ID.String())
	render.Status(r, http.StatusCreated)
	render.JSON(w, r, resp)
}
