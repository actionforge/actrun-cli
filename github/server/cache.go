package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type CacheEntry struct {
	ID        int64
	Key       string
	Version   string
	Scope     string
	Size      int64
	Finalized bool
	CreatedAt time.Time
}

// --- Twirp dispatcher ---

func (s *Server) handleCacheTwirp(w http.ResponseWriter, r *http.Request) {
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		writeTwirpError(w, http.StatusBadRequest, "invalid_argument", "Content-Type must be application/json")
		return
	}

	if _, _, err := parseJWT(r.Header.Get("Authorization")); err != nil {
		writeTwirpError(w, http.StatusUnauthorized, "unauthenticated", err.Error())
		return
	}

	method := r.PathValue("method")
	switch method {
	case "CreateCacheEntry":
		s.handleCreateCacheEntry(w, r)
	case "FinalizeCacheEntryUpload":
		s.handleFinalizeCacheEntry(w, r)
	case "GetCacheEntryDownloadURL":
		s.handleGetCacheEntryDownloadURL(w, r)
	case "DeleteCacheEntry":
		s.handleDeleteCacheEntry(w, r)
	default:
		writeTwirpError(w, http.StatusNotFound, "not_found", fmt.Sprintf("unknown method: %s", method))
	}
}

// --- Request/Response types ---

type CacheMetadata struct {
	RepositoryID string `json:"repository_id"`
	Scope        string `json:"scope"`
}

type CreateCacheEntryRequest struct {
	Key      string         `json:"key"`
	Version  string         `json:"version"`
	Metadata *CacheMetadata `json:"metadata,omitempty"`
}

type CreateCacheEntryResponse struct {
	Ok              bool   `json:"ok"`
	SignedUploadURL string `json:"signed_upload_url"`
}

// FlexInt64 unmarshals from both JSON numbers and JSON strings.
// Protobuf's canonical JSON encoding represents int64 as strings.
type FlexInt64 int64

func (f *FlexInt64) UnmarshalJSON(data []byte) error {
	var n int64
	if err := json.Unmarshal(data, &n); err == nil {
		*f = FlexInt64(n)
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("FlexInt64: cannot unmarshal %s", string(data))
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("FlexInt64: invalid int64 string %q: %w", s, err)
	}
	*f = FlexInt64(n)
	return nil
}

type FinalizeCacheEntryRequest struct {
	Key       string    `json:"key"`
	Version   string    `json:"version"`
	SizeBytes FlexInt64 `json:"size_bytes"`
}

type FinalizeCacheEntryResponse struct {
	Ok      bool   `json:"ok"`
	EntryID string `json:"entry_id"`
}

type GetCacheEntryDownloadURLRequest struct {
	Metadata    *CacheMetadata `json:"metadata,omitempty"`
	Key         string         `json:"key"`
	RestoreKeys []string       `json:"restore_keys,omitempty"`
	Version     string         `json:"version"`
}

type GetCacheEntryDownloadURLResponse struct {
	Ok                bool   `json:"ok"`
	SignedDownloadURL string `json:"signed_download_url"`
	MatchedKey        string `json:"matched_key"`
}

type DeleteCacheEntryRequest struct {
	Key     string `json:"key"`
	Version string `json:"version"`
}

type DeleteCacheEntryResponse struct {
	Ok bool `json:"ok"`
}

// --- RPC handlers ---

func (s *Server) handleCreateCacheEntry(w http.ResponseWriter, r *http.Request) {
	var req CreateCacheEntryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeTwirpError(w, http.StatusBadRequest, "invalid_argument", "invalid JSON")
		return
	}
	if req.Key == "" || req.Version == "" {
		writeTwirpError(w, http.StatusBadRequest, "invalid_argument", "key and version are required")
		return
	}

	scope := ""
	if req.Metadata != nil {
		scope = req.Metadata.Scope
	}
	cacheKey := scope + "/" + req.Key + "/" + req.Version

	s.mu.Lock()
	// If entry already exists, overwrite (caches are mutable)
	if existing, ok := s.caches[cacheKey]; ok {
		delete(s.cacheByID, existing.ID)
		delete(s.uploadMu, existing.ID)
	}
	id := s.nextID
	s.nextID++
	entry := &CacheEntry{
		ID:        id,
		Key:       req.Key,
		Version:   req.Version,
		Scope:     scope,
		CreatedAt: time.Now(),
	}
	s.caches[cacheKey] = entry
	s.cacheByID[id] = entry
	s.uploadMu[id] = &sync.Mutex{}
	s.mu.Unlock()

	uploadURL := s.makeSignedURL("PUT", id)
	writeJSON(w, http.StatusOK, CreateCacheEntryResponse{
		Ok:              true,
		SignedUploadURL: uploadURL,
	})
}

func (s *Server) handleFinalizeCacheEntry(w http.ResponseWriter, r *http.Request) {
	var req FinalizeCacheEntryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeTwirpError(w, http.StatusBadRequest, "invalid_argument", "invalid JSON")
		return
	}

	s.mu.Lock()
	var found *CacheEntry
	for _, entry := range s.caches {
		if entry.Key == req.Key && entry.Version == req.Version {
			found = entry
			break
		}
	}
	if found == nil {
		s.mu.Unlock()
		writeTwirpError(w, http.StatusNotFound, "not_found", "cache entry not found")
		return
	}
	found.Size = int64(req.SizeBytes)
	found.Finalized = true
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, FinalizeCacheEntryResponse{
		Ok:      true,
		EntryID: strconv.FormatInt(found.ID, 10),
	})
}

func (s *Server) handleGetCacheEntryDownloadURL(w http.ResponseWriter, r *http.Request) {
	var req GetCacheEntryDownloadURLRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeTwirpError(w, http.StatusBadRequest, "invalid_argument", "invalid JSON")
		return
	}

	scope := ""
	if req.Metadata != nil {
		scope = req.Metadata.Scope
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	// 1. Exact match: scope + key + version
	exactKey := scope + "/" + req.Key + "/" + req.Version
	if entry, ok := s.caches[exactKey]; ok && entry.Finalized {
		downloadURL := s.makeSignedURL("GET", entry.ID)
		writeJSON(w, http.StatusOK, GetCacheEntryDownloadURLResponse{
			Ok:                true,
			SignedDownloadURL: downloadURL,
			MatchedKey:        entry.Key,
		})
		return
	}

	// 2. Prefix match with restore_keys
	for _, rk := range req.RestoreKeys {
		var best *CacheEntry
		for _, entry := range s.caches {
			if entry.Scope != scope || entry.Version != req.Version {
				continue
			}
			if !entry.Finalized {
				continue
			}
			if !strings.HasPrefix(entry.Key, rk) {
				continue
			}
			if best == nil || entry.CreatedAt.After(best.CreatedAt) {
				best = entry
			}
		}
		if best != nil {
			downloadURL := s.makeSignedURL("GET", best.ID)
			writeJSON(w, http.StatusOK, GetCacheEntryDownloadURLResponse{
				Ok:                true,
				SignedDownloadURL: downloadURL,
				MatchedKey:        best.Key,
			})
			return
		}
	}

	writeTwirpError(w, http.StatusNotFound, "not_found", "cache entry not found")
}

func (s *Server) handleDeleteCacheEntry(w http.ResponseWriter, r *http.Request) {
	var req DeleteCacheEntryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeTwirpError(w, http.StatusBadRequest, "invalid_argument", "invalid JSON")
		return
	}

	s.mu.Lock()
	var found *CacheEntry
	var foundKey string
	for k, entry := range s.caches {
		if entry.Key == req.Key && entry.Version == req.Version {
			found = entry
			foundKey = k
			break
		}
	}
	if found == nil {
		s.mu.Unlock()
		writeTwirpError(w, http.StatusNotFound, "not_found", "cache entry not found")
		return
	}
	delete(s.caches, foundKey)
	delete(s.cacheByID, found.ID)
	delete(s.uploadMu, found.ID)
	s.mu.Unlock()

	blobPath := filepath.Join(s.storageDir, fmt.Sprintf("%d.blob", found.ID))
	os.Remove(blobPath)

	writeJSON(w, http.StatusOK, DeleteCacheEntryResponse{Ok: true})
}
