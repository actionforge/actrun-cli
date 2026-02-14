package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Artifact represents a stored artifact with its metadata.
type Artifact struct {
	ID              int64
	Name            string
	RunBackendID    string
	JobBackendID    string
	BlobPath        string // on-disk path: {storageDir}/{runBackendID}/{name}
	Size            int64
	Hash            string
	Finalized       bool
	CreatedAt       time.Time
	ExpiresAt       time.Time
	WorkflowName    string
	RunID           int64
	WorkflowRunID   int64
	MonolithRunID   int64
}

// Server implements the GitHub Actions artifact service protocol.
type Server struct {
	mu          sync.RWMutex
	// Artifact v4 state
	artifacts   map[string]*Artifact          // "{runBackendId}/{name}"
	artByID     map[int64]*Artifact
	// Cache state
	caches      map[string]*CacheEntry        // "{scope}/{key}/{version}"
	cacheByID   map[int64]*CacheEntry
	// Legacy artifact v3 state
	containers  map[string]*ArtifactContainer // "{runId}/{name}"
	contByID    map[int64]*ArtifactContainer
	// Shared
	uploadMu    map[int64]*sync.Mutex
	nextID      int64
	storageDir  string
	signingKey  []byte
	externalURL string
	oidcCfg     OIDCConfig
	mux         *http.ServeMux
}

const signedURLTTL = 60 * time.Minute

// artifactBlobPath returns the on-disk storage path for an artifact.
// Layout: {storageDir}/{runBackendID}/{artifactName}
func (s *Server) artifactBlobPath(runBackendID, name string) string {
	// Sanitize to prevent path traversal
	safeRun := filepath.Base(runBackendID)
	safeName := filepath.Base(name)
	return filepath.Join(s.storageDir, safeRun, safeName)
}

// getBlobPath returns the on-disk path for an artifact by its numeric ID.
// Caller must hold at least s.mu.RLock.
func (s *Server) getBlobPath(id int64) string {
	if art, ok := s.artByID[id]; ok && art.BlobPath != "" {
		return art.BlobPath
	}
	// Fallback for legacy/cache entries that don't have BlobPath set
	return filepath.Join(s.storageDir, fmt.Sprintf("%d.blob", id))
}

func NewServer(storageDir string, signingKey []byte, externalURL string, oidcCfg OIDCConfig) *Server {
	s := &Server{
		artifacts:   make(map[string]*Artifact),
		artByID:     make(map[int64]*Artifact),
		caches:      make(map[string]*CacheEntry),
		cacheByID:   make(map[int64]*CacheEntry),
		containers:  make(map[string]*ArtifactContainer),
		contByID:    make(map[int64]*ArtifactContainer),
		uploadMu:    make(map[int64]*sync.Mutex),
		nextID:      1,
		storageDir:  storageDir,
		signingKey:  signingKey,
		externalURL: strings.TrimRight(externalURL, "/"),
		oidcCfg:     oidcCfg,
		mux:         http.NewServeMux(),
	}
	// Artifact v4 (Twirp)
	s.mux.HandleFunc("POST /twirp/github.actions.results.api.v1.ArtifactService/{method}", s.handleTwirp)
	// Cache (Twirp)
	s.mux.HandleFunc("POST /twirp/github.actions.results.api.v1.CacheService/{method}", s.handleCacheTwirp)
	// Blob upload/download (shared by v4 and cache)
	s.mux.HandleFunc("PUT /upload/{id}", s.handleBlobUpload)
	s.mux.HandleFunc("GET /download/{id}", s.handleBlobDownload)
	// OIDC token service
	s.mux.HandleFunc("GET /_services/token", s.handleOIDCToken)
	// Legacy v3 artifact API
	s.mux.HandleFunc("POST /_apis/pipelines/workflows/{runId}/artifacts", s.handleLegacyCreate)
	s.mux.HandleFunc("PATCH /_apis/pipelines/workflows/{runId}/artifacts", s.handleLegacyFinalize)
	s.mux.HandleFunc("GET /_apis/pipelines/workflows/{runId}/artifacts", s.handleLegacyList)
	s.mux.HandleFunc("PUT /v3-upload/{containerId}", s.handleLegacyUpload)
	s.mux.HandleFunc("GET /download-v3/{containerId}", s.handleLegacyListFiles)
	s.mux.HandleFunc("GET /artifact/{path...}", s.handleLegacyDownload)
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// --- Twirp dispatcher ---

func (s *Server) handleTwirp(w http.ResponseWriter, r *http.Request) {
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		writeTwirpError(w, http.StatusBadRequest, "invalid_argument", "Content-Type must be application/json")
		return
	}

	runID, jobID, err := parseJWT(r.Header.Get("Authorization"))
	if err != nil {
		writeTwirpError(w, http.StatusUnauthorized, "unauthenticated", err.Error())
		return
	}

	method := r.PathValue("method")
	switch method {
	case "CreateArtifact":
		s.handleCreateArtifact(w, r, runID, jobID)
	case "FinalizeArtifact":
		s.handleFinalizeArtifact(w, r, runID, jobID)
	case "ListArtifacts":
		s.handleListArtifacts(w, r, runID)
	case "GetSignedArtifactURL":
		s.handleGetSignedArtifactURL(w, r, runID)
	case "DeleteArtifact":
		s.handleDeleteArtifact(w, r, runID)
	case "MigrateArtifact":
		s.handleMigrateArtifact(w, r, runID)
	case "FinalizeMigratedArtifact":
		s.handleFinalizeMigratedArtifact(w, r, runID)
	default:
		writeTwirpError(w, http.StatusNotFound, "not_found", fmt.Sprintf("unknown method: %s", method))
	}
}

// --- Request/Response types ---

type CreateArtifactRequest struct {
	WorkflowRunBackendID    string  `json:"workflow_run_backend_id"`
	WorkflowJobRunBackendID string  `json:"workflow_job_run_backend_id"`
	Name                    string  `json:"name"`
	Version                 int     `json:"version"`
	ExpiresAt               *string `json:"expires_at,omitempty"`
}

type CreateArtifactResponse struct {
	Ok              bool   `json:"ok"`
	SignedUploadURL string `json:"signed_upload_url"`
}

type FinalizeArtifactRequest struct {
	WorkflowRunBackendID    string  `json:"workflow_run_backend_id"`
	WorkflowJobRunBackendID string  `json:"workflow_job_run_backend_id"`
	Name                    string  `json:"name"`
	Size                    string  `json:"size"`
	Hash                    *string `json:"hash,omitempty"`
}

type FinalizeArtifactResponse struct {
	Ok         bool   `json:"ok"`
	ArtifactID string `json:"artifact_id"`
}

type ListArtifactsRequest struct {
	WorkflowRunBackendID    string  `json:"workflow_run_backend_id"`
	WorkflowJobRunBackendID string  `json:"workflow_job_run_backend_id"`
	NameFilter              *string `json:"name_filter,omitempty"`
	IDFilter                *string `json:"id_filter,omitempty"`
}

type ListArtifactsResponse struct {
	Artifacts []ArtifactEntry `json:"artifacts"`
}

type ArtifactEntry struct {
	WorkflowRunBackendID    string  `json:"workflow_run_backend_id"`
	WorkflowJobRunBackendID string  `json:"workflow_job_run_backend_id"`
	DatabaseID              string  `json:"database_id"`
	Name                    string  `json:"name"`
	Size                    string  `json:"size"`
	CreatedAt               *string `json:"created_at,omitempty"`
	Digest                  *string `json:"digest,omitempty"`
}

type GetSignedArtifactURLRequest struct {
	WorkflowRunBackendID    string `json:"workflow_run_backend_id"`
	WorkflowJobRunBackendID string `json:"workflow_job_run_backend_id"`
	Name                    string `json:"name"`
}

type GetSignedArtifactURLResponse struct {
	SignedURL string `json:"signed_url"`
}

type DeleteArtifactRequest struct {
	WorkflowRunBackendID    string `json:"workflow_run_backend_id"`
	WorkflowJobRunBackendID string `json:"workflow_job_run_backend_id"`
	Name                    string `json:"name"`
}

type DeleteArtifactResponse struct {
	Ok         bool   `json:"ok"`
	ArtifactID string `json:"artifact_id"`
}

type MigrateArtifactRequest struct {
	WorkflowRunBackendID string  `json:"workflow_run_backend_id"`
	Name                 string  `json:"name"`
	ExpiresAt            *string `json:"expires_at,omitempty"`
}

type MigrateArtifactResponse struct {
	Ok              bool   `json:"ok"`
	SignedUploadURL string `json:"signed_upload_url"`
}

type FinalizeMigratedArtifactRequest struct {
	WorkflowRunBackendID string `json:"workflow_run_backend_id"`
	Name                 string `json:"name"`
	Size                 string `json:"size"`
}

type FinalizeMigratedArtifactResponse struct {
	Ok         bool   `json:"ok"`
	ArtifactID string `json:"artifact_id"`
}

// --- Twirp RPC handlers ---

func (s *Server) handleCreateArtifact(w http.ResponseWriter, r *http.Request, runID, jobID string) {
	var req CreateArtifactRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeTwirpError(w, http.StatusBadRequest, "invalid_argument", "invalid JSON")
		return
	}

	if req.Name == "" {
		writeTwirpError(w, http.StatusBadRequest, "invalid_argument", "name is required")
		return
	}
	if req.WorkflowRunBackendID == "" {
		req.WorkflowRunBackendID = runID
	}
	if req.WorkflowJobRunBackendID == "" {
		req.WorkflowJobRunBackendID = jobID
	}

	key := req.WorkflowRunBackendID + "/" + req.Name

	blobPath := s.artifactBlobPath(req.WorkflowRunBackendID, req.Name)

	s.mu.Lock()
	if existing, ok := s.artifacts[key]; ok && existing.Finalized {
		s.mu.Unlock()
		writeTwirpError(w, http.StatusConflict, "already_exists",
			fmt.Sprintf("an artifact with this name already exists on the workflow run: %s", req.Name))
		return
	}
	id := s.nextID
	s.nextID++
	art := &Artifact{
		ID:           id,
		Name:         req.Name,
		RunBackendID: req.WorkflowRunBackendID,
		JobBackendID: req.WorkflowJobRunBackendID,
		BlobPath:     blobPath,
		CreatedAt:    time.Now(),
	}
	if req.ExpiresAt != nil {
		if t, err := time.Parse(time.RFC3339, *req.ExpiresAt); err == nil {
			art.ExpiresAt = t
		}
	}
	s.artifacts[key] = art
	s.artByID[id] = art
	s.uploadMu[id] = &sync.Mutex{}
	s.mu.Unlock()

	// Ensure run subdirectory exists
	if err := os.MkdirAll(filepath.Dir(blobPath), 0o755); err != nil {
		writeTwirpError(w, http.StatusInternalServerError, "internal", "failed to create storage directory")
		return
	}

	uploadURL := s.makeSignedURL("PUT", id)

	writeJSON(w, http.StatusOK, CreateArtifactResponse{
		Ok:              true,
		SignedUploadURL: uploadURL,
	})
}

func (s *Server) handleFinalizeArtifact(w http.ResponseWriter, r *http.Request, runID, _ string) {
	var req FinalizeArtifactRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeTwirpError(w, http.StatusBadRequest, "invalid_argument", "invalid JSON")
		return
	}
	if req.WorkflowRunBackendID == "" {
		req.WorkflowRunBackendID = runID
	}

	key := req.WorkflowRunBackendID + "/" + req.Name

	s.mu.Lock()
	art, ok := s.artifacts[key]
	if !ok {
		s.mu.Unlock()
		writeTwirpError(w, http.StatusNotFound, "not_found", fmt.Sprintf("artifact %q not found", req.Name))
		return
	}

	size, _ := strconv.ParseInt(req.Size, 10, 64)
	art.Size = size
	if req.Hash != nil {
		art.Hash = *req.Hash
	}
	art.Finalized = true
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, FinalizeArtifactResponse{
		Ok:         true,
		ArtifactID: strconv.FormatInt(art.ID, 10),
	})
}

func (s *Server) handleListArtifacts(w http.ResponseWriter, r *http.Request, runID string) {
	var req ListArtifactsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeTwirpError(w, http.StatusBadRequest, "invalid_argument", "invalid JSON")
		return
	}
	if req.WorkflowRunBackendID == "" {
		req.WorkflowRunBackendID = runID
	}

	s.mu.RLock()
	var entries []ArtifactEntry
	for _, art := range s.artifacts {
		if art.RunBackendID != req.WorkflowRunBackendID {
			continue
		}
		if !art.Finalized {
			continue
		}
		if req.NameFilter != nil && art.Name != *req.NameFilter {
			continue
		}
		if req.IDFilter != nil {
			filterID, _ := strconv.ParseInt(*req.IDFilter, 10, 64)
			if art.ID != filterID {
				continue
			}
		}
		ts := art.CreatedAt.UTC().Format(time.RFC3339)
		entry := ArtifactEntry{
			WorkflowRunBackendID:    art.RunBackendID,
			WorkflowJobRunBackendID: art.JobBackendID,
			DatabaseID:              strconv.FormatInt(art.ID, 10),
			Name:                    art.Name,
			Size:                    strconv.FormatInt(art.Size, 10),
			CreatedAt:               &ts,
		}
		if art.Hash != "" {
			h := art.Hash
			entry.Digest = &h
		}
		entries = append(entries, entry)
	}
	s.mu.RUnlock()

	if entries == nil {
		entries = []ArtifactEntry{}
	}

	writeJSON(w, http.StatusOK, ListArtifactsResponse{Artifacts: entries})
}

func (s *Server) handleGetSignedArtifactURL(w http.ResponseWriter, r *http.Request, runID string) {
	var req GetSignedArtifactURLRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeTwirpError(w, http.StatusBadRequest, "invalid_argument", "invalid JSON")
		return
	}
	if req.WorkflowRunBackendID == "" {
		req.WorkflowRunBackendID = runID
	}

	key := req.WorkflowRunBackendID + "/" + req.Name

	s.mu.RLock()
	art, ok := s.artifacts[key]
	s.mu.RUnlock()

	if !ok || !art.Finalized {
		writeTwirpError(w, http.StatusNotFound, "not_found", fmt.Sprintf("artifact %q not found", req.Name))
		return
	}

	downloadURL := s.makeSignedURL("GET", art.ID)

	writeJSON(w, http.StatusOK, GetSignedArtifactURLResponse{
		SignedURL: downloadURL,
	})
}

func (s *Server) handleDeleteArtifact(w http.ResponseWriter, r *http.Request, runID string) {
	var req DeleteArtifactRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeTwirpError(w, http.StatusBadRequest, "invalid_argument", "invalid JSON")
		return
	}
	if req.WorkflowRunBackendID == "" {
		req.WorkflowRunBackendID = runID
	}

	key := req.WorkflowRunBackendID + "/" + req.Name

	s.mu.Lock()
	art, ok := s.artifacts[key]
	if !ok {
		s.mu.Unlock()
		writeTwirpError(w, http.StatusNotFound, "not_found", fmt.Sprintf("artifact %q not found", req.Name))
		return
	}
	delete(s.artifacts, key)
	delete(s.artByID, art.ID)
	delete(s.uploadMu, art.ID)
	s.mu.Unlock()

	os.Remove(art.BlobPath)

	writeJSON(w, http.StatusOK, DeleteArtifactResponse{
		Ok:         true,
		ArtifactID: strconv.FormatInt(art.ID, 10),
	})
}

func (s *Server) handleMigrateArtifact(w http.ResponseWriter, r *http.Request, runID string) {
	var req MigrateArtifactRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeTwirpError(w, http.StatusBadRequest, "invalid_argument", "invalid JSON")
		return
	}

	if req.Name == "" {
		writeTwirpError(w, http.StatusBadRequest, "invalid_argument", "name is required")
		return
	}
	if req.WorkflowRunBackendID == "" {
		req.WorkflowRunBackendID = runID
	}

	key := req.WorkflowRunBackendID + "/" + req.Name
	blobPath := s.artifactBlobPath(req.WorkflowRunBackendID, req.Name)

	s.mu.Lock()
	if existing, ok := s.artifacts[key]; ok && existing.Finalized {
		s.mu.Unlock()
		writeTwirpError(w, http.StatusConflict, "already_exists",
			fmt.Sprintf("an artifact with this name already exists on the workflow run: %s", req.Name))
		return
	}
	id := s.nextID
	s.nextID++
	art := &Artifact{
		ID:           id,
		Name:         req.Name,
		RunBackendID: req.WorkflowRunBackendID,
		BlobPath:     blobPath,
		CreatedAt:    time.Now(),
	}
	s.artifacts[key] = art
	s.artByID[id] = art
	s.uploadMu[id] = &sync.Mutex{}
	s.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(blobPath), 0o755); err != nil {
		writeTwirpError(w, http.StatusInternalServerError, "internal", "failed to create storage directory")
		return
	}

	uploadURL := s.makeSignedURL("PUT", id)

	writeJSON(w, http.StatusOK, MigrateArtifactResponse{
		Ok:              true,
		SignedUploadURL: uploadURL,
	})
}

func (s *Server) handleFinalizeMigratedArtifact(w http.ResponseWriter, r *http.Request, runID string) {
	var req FinalizeMigratedArtifactRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeTwirpError(w, http.StatusBadRequest, "invalid_argument", "invalid JSON")
		return
	}
	if req.WorkflowRunBackendID == "" {
		req.WorkflowRunBackendID = runID
	}

	key := req.WorkflowRunBackendID + "/" + req.Name

	s.mu.Lock()
	art, ok := s.artifacts[key]
	if !ok {
		s.mu.Unlock()
		writeTwirpError(w, http.StatusNotFound, "not_found", fmt.Sprintf("artifact %q not found", req.Name))
		return
	}

	size, _ := strconv.ParseInt(req.Size, 10, 64)
	art.Size = size
	art.Finalized = true
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, FinalizeMigratedArtifactResponse{
		Ok:         true,
		ArtifactID: strconv.FormatInt(art.ID, 10),
	})
}

// --- Blob upload/download ---

func (s *Server) handleBlobUpload(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid artifact ID", http.StatusBadRequest)
		return
	}

	if err := s.verifySignedURL(r, "PUT", id); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}

	s.mu.RLock()
	mu, ok := s.uploadMu[id]
	s.mu.RUnlock()
	if !ok {
		http.Error(w, "artifact not found", http.StatusNotFound)
		return
	}

	comp := r.URL.Query().Get("comp")
	s.mu.RLock()
	blobPath := s.getBlobPath(id)
	s.mu.RUnlock()

	switch comp {
	case "block":
		mu.Lock()
		defer mu.Unlock()
		f, err := os.OpenFile(blobPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			http.Error(w, "storage error", http.StatusInternalServerError)
			return
		}
		defer f.Close()
		if _, err := io.Copy(f, r.Body); err != nil {
			http.Error(w, "storage error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)

	case "blocklist":
		w.WriteHeader(http.StatusCreated)

	default:
		mu.Lock()
		defer mu.Unlock()
		f, err := os.OpenFile(blobPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
		if err != nil {
			http.Error(w, "storage error", http.StatusInternalServerError)
			return
		}
		defer f.Close()
		if _, err := io.Copy(f, r.Body); err != nil {
			http.Error(w, "storage error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
	}
}

func (s *Server) handleBlobDownload(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid artifact ID", http.StatusBadRequest)
		return
	}

	if err := s.verifySignedURL(r, "GET", id); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}

	s.mu.RLock()
	blobPath := s.getBlobPath(id)
	s.mu.RUnlock()
	f, err := os.Open(blobPath)
	if err != nil {
		http.Error(w, "blob not found", http.StatusNotFound)
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", "application/octet-stream")
	io.Copy(w, f)
}

// --- Signed URL creation and verification ---

func (s *Server) makeSignedURL(method string, artifactID int64) string {
	exp := time.Now().Add(signedURLTTL).Unix()
	sig := s.computeSignature(method, artifactID, exp)

	var pathPrefix string
	if method == "PUT" {
		pathPrefix = "upload"
	} else {
		pathPrefix = "download"
	}

	return fmt.Sprintf("%s/%s/%d?sig=%s&exp=%d", s.externalURL, pathPrefix, artifactID, sig, exp)
}

func (s *Server) computeSignature(method string, artifactID int64, exp int64) string {
	msg := fmt.Sprintf("%s:%d:%d", method, artifactID, exp)
	mac := hmac.New(sha256.New, s.signingKey)
	mac.Write([]byte(msg))
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *Server) verifySignedURL(r *http.Request, method string, artifactID int64) error {
	sig := r.URL.Query().Get("sig")
	expStr := r.URL.Query().Get("exp")
	if sig == "" || expStr == "" {
		return fmt.Errorf("missing signature parameters")
	}
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid expiry")
	}
	if time.Now().Unix() > exp {
		return fmt.Errorf("signed URL expired")
	}
	expected := s.computeSignature(method, artifactID, exp)
	if !hmac.Equal([]byte(sig), []byte(expected)) {
		return fmt.Errorf("invalid signature")
	}
	return nil
}

// --- JWT parsing ---

func parseJWT(authHeader string) (runID, jobID string, err error) {
	if authHeader == "" {
		return "", "", fmt.Errorf("missing Authorization header")
	}
	token := strings.TrimPrefix(authHeader, "Bearer ")
	if token == authHeader {
		return "", "", fmt.Errorf("invalid Authorization header format")
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", "", fmt.Errorf("invalid JWT format")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", "", fmt.Errorf("invalid JWT payload encoding: %w", err)
	}

	var claims struct {
		Scp string `json:"scp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", "", fmt.Errorf("invalid JWT payload: %w", err)
	}

	// Look for Actions.Results:{runId}:{jobId}
	for _, scope := range strings.Fields(claims.Scp) {
		if strings.HasPrefix(scope, "Actions.Results:") {
			parts := strings.SplitN(scope, ":", 3)
			if len(parts) == 3 {
				return parts[1], parts[2], nil
			}
			if len(parts) == 2 {
				return parts[1], "", nil
			}
		}
	}

	return "", "", fmt.Errorf("no Actions.Results scope in JWT")
}

// --- JSON helpers ---

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeTwirpError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{
		"code": code,
		"msg":  msg,
	})
}
