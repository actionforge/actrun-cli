package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type ArtifactContainer struct {
	ID        int64
	RunID     string
	Name      string
	Files     map[string]*ContainerFile
	Finalized bool
}

type ContainerFile struct {
	Path        string
	Size        int64
	ContentGzip bool
}

// POST /_apis/pipelines/workflows/{runId}/artifacts
func (s *Server) handleLegacyCreate(w http.ResponseWriter, r *http.Request) {
	if !hasBearer(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	runID := r.PathValue("runId")
	var req struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	key := runID + "/" + req.Name

	s.mu.Lock()
	id := s.nextID
	s.nextID++
	container := &ArtifactContainer{
		ID:    id,
		RunID: runID,
		Name:  req.Name,
		Files: make(map[string]*ContainerFile),
	}
	s.containers[key] = container
	s.contByID[id] = container
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"fileContainerResourceUrl": fmt.Sprintf("%s/v3-upload/%d", s.externalURL, id),
	})
}

// PUT /v3-upload/{containerId}?itemPath={path}
func (s *Server) handleLegacyUpload(w http.ResponseWriter, r *http.Request) {
	cidStr := r.PathValue("containerId")
	cid, err := strconv.ParseInt(cidStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid container ID", http.StatusBadRequest)
		return
	}

	s.mu.RLock()
	container, ok := s.contByID[cid]
	s.mu.RUnlock()
	if !ok {
		http.Error(w, "container not found", http.StatusNotFound)
		return
	}

	itemPath := r.URL.Query().Get("itemPath")
	if itemPath == "" {
		http.Error(w, "itemPath is required", http.StatusBadRequest)
		return
	}

	dir := filepath.Join(s.storageDir, "v3", cidStr, filepath.Dir(itemPath))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		http.Error(w, "storage error", http.StatusInternalServerError)
		return
	}

	filePath := filepath.Join(s.storageDir, "v3", cidStr, itemPath)

	// Parse Content-Range for chunked uploads: "bytes {start}-{end}/{total}"
	var start int64
	if cr := r.Header.Get("Content-Range"); cr != "" {
		fmt.Sscanf(cr, "bytes %d-", &start)
	}

	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		http.Error(w, "storage error", http.StatusInternalServerError)
		return
	}
	if start > 0 {
		f.Seek(start, io.SeekStart)
	}
	n, copyErr := io.Copy(f, r.Body)
	f.Close()
	if copyErr != nil {
		http.Error(w, "storage error", http.StatusInternalServerError)
		return
	}

	isGzip := r.Header.Get("Content-Encoding") == "gzip"

	s.mu.Lock()
	cf := container.Files[itemPath]
	if cf == nil {
		cf = &ContainerFile{Path: itemPath}
		container.Files[itemPath] = cf
	}
	cf.Size = start + n
	cf.ContentGzip = isGzip
	s.mu.Unlock()

	writeJSON(w, http.StatusCreated, map[string]string{"message": "success"})
}

// PATCH /_apis/pipelines/workflows/{runId}/artifacts?artifactName={name}
func (s *Server) handleLegacyFinalize(w http.ResponseWriter, r *http.Request) {
	if !hasBearer(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	runID := r.PathValue("runId")
	name := r.URL.Query().Get("artifactName")

	s.mu.Lock()
	for _, container := range s.containers {
		if container.RunID != runID {
			continue
		}
		if name != "" && container.Name != name {
			continue
		}
		container.Finalized = true
	}
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]string{"message": "success"})
}

// GET /_apis/pipelines/workflows/{runId}/artifacts
func (s *Server) handleLegacyList(w http.ResponseWriter, r *http.Request) {
	if !hasBearer(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	runID := r.PathValue("runId")

	s.mu.RLock()
	var items []map[string]any
	for _, container := range s.containers {
		if container.RunID != runID || !container.Finalized {
			continue
		}
		items = append(items, map[string]any{
			"name":                     container.Name,
			"fileContainerResourceUrl": fmt.Sprintf("%s/download-v3/%d", s.externalURL, container.ID),
		})
	}
	s.mu.RUnlock()

	if items == nil {
		items = []map[string]any{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"count": len(items),
		"value": items,
	})
}

// GET /download-v3/{containerId}?itemPath={prefix}
func (s *Server) handleLegacyListFiles(w http.ResponseWriter, r *http.Request) {
	cidStr := r.PathValue("containerId")
	cid, err := strconv.ParseInt(cidStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid container ID", http.StatusBadRequest)
		return
	}

	s.mu.RLock()
	container, ok := s.contByID[cid]
	if !ok {
		s.mu.RUnlock()
		http.Error(w, "container not found", http.StatusNotFound)
		return
	}

	prefix := r.URL.Query().Get("itemPath")
	var items []map[string]any
	for _, file := range container.Files {
		if prefix != "" && !strings.HasPrefix(file.Path, prefix) {
			continue
		}
		items = append(items, map[string]any{
			"path":            file.Path,
			"itemType":        "file",
			"contentLocation": fmt.Sprintf("%s/artifact/%d/%s", s.externalURL, cid, file.Path),
		})
	}
	s.mu.RUnlock()

	if items == nil {
		items = []map[string]any{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"value": items,
	})
}

// GET /artifact/{path...}
func (s *Server) handleLegacyDownload(w http.ResponseWriter, r *http.Request) {
	fullPath := r.PathValue("path")
	idx := strings.IndexByte(fullPath, '/')
	if idx < 0 {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	cidStr := fullPath[:idx]
	filePath := fullPath[idx+1:]

	cid, err := strconv.ParseInt(cidStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid container ID", http.StatusBadRequest)
		return
	}

	s.mu.RLock()
	container, ok := s.contByID[cid]
	if !ok {
		s.mu.RUnlock()
		http.Error(w, "container not found", http.StatusNotFound)
		return
	}
	cf, ok := container.Files[filePath]
	s.mu.RUnlock()
	if !ok {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}

	diskPath := filepath.Join(s.storageDir, "v3", cidStr, filePath)
	f, err := os.Open(diskPath)
	if err != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	defer f.Close()

	if cf.ContentGzip {
		w.Header().Set("Content-Encoding", "gzip")
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	io.Copy(w, f)
}

func hasBearer(r *http.Request) bool {
	auth := r.Header.Get("Authorization")
	return strings.HasPrefix(auth, "Bearer ") && len(auth) > 7
}
