package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func cacheTwirpRequest(t *testing.T, ts *httptest.Server, method string, token string, body any) *http.Response {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	url := ts.URL + "/twirp/github.actions.results.api.v1.CacheService/" + method
	req, err := http.NewRequest("POST", url, bytes.NewReader(b))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

func TestCacheFullCycle(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	token := makeTestJWT("Actions.Results:run1:job1")
	blobContent := []byte("cached data here")

	// 1. CreateCacheEntry
	resp := cacheTwirpRequest(t, ts, "CreateCacheEntry", token, map[string]any{
		"key":     "node-modules-abc123",
		"version": "v1",
		"metadata": map[string]string{
			"scope": "refs/heads/main",
		},
	})
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("CreateCacheEntry status=%d body=%s", resp.StatusCode, body)
	}
	createResp := decodeResponse[CreateCacheEntryResponse](t, resp)
	if !createResp.Ok || createResp.SignedUploadURL == "" {
		t.Fatalf("CreateCacheEntry failed: %+v", createResp)
	}

	// 2. Upload blob
	req, _ := http.NewRequest("PUT", createResp.SignedUploadURL, bytes.NewReader(blobContent))
	uploadResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("upload blob: %v", err)
	}
	uploadResp.Body.Close()
	if uploadResp.StatusCode != http.StatusCreated {
		t.Fatalf("upload status=%d", uploadResp.StatusCode)
	}

	// 3. FinalizeCacheEntryUpload
	resp = cacheTwirpRequest(t, ts, "FinalizeCacheEntryUpload", token, map[string]any{
		"key":        "node-modules-abc123",
		"version":    "v1",
		"size_bytes": len(blobContent),
	})
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("FinalizeCacheEntryUpload status=%d body=%s", resp.StatusCode, body)
	}
	finalResp := decodeResponse[FinalizeCacheEntryResponse](t, resp)
	if !finalResp.Ok {
		t.Fatal("FinalizeCacheEntryUpload ok=false")
	}

	// 4. GetCacheEntryDownloadURL
	resp = cacheTwirpRequest(t, ts, "GetCacheEntryDownloadURL", token, map[string]any{
		"key":     "node-modules-abc123",
		"version": "v1",
		"metadata": map[string]string{
			"scope": "refs/heads/main",
		},
	})
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("GetCacheEntryDownloadURL status=%d body=%s", resp.StatusCode, body)
	}
	dlURLResp := decodeResponse[GetCacheEntryDownloadURLResponse](t, resp)
	if !dlURLResp.Ok || dlURLResp.SignedDownloadURL == "" {
		t.Fatalf("GetCacheEntryDownloadURL failed: %+v", dlURLResp)
	}

	// 5. Download blob
	getResp, err := http.Get(dlURLResp.SignedDownloadURL)
	if err != nil {
		t.Fatalf("download blob: %v", err)
	}
	defer getResp.Body.Close()
	data, _ := io.ReadAll(getResp.Body)
	if !bytes.Equal(data, blobContent) {
		t.Fatalf("cache data mismatch: got %q, want %q", data, blobContent)
	}
}

func TestCachePrefixMatch(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	token := makeTestJWT("Actions.Results:run1:job1")
	scope := "refs/heads/main"

	// Create and finalize two entries with different keys but same prefix
	for _, key := range []string{"node-modules-aaa", "node-modules-bbb"} {
		resp := cacheTwirpRequest(t, ts, "CreateCacheEntry", token, map[string]any{
			"key":      key,
			"version":  "v1",
			"metadata": map[string]string{"scope": scope},
		})
		createResp := decodeResponse[CreateCacheEntryResponse](t, resp)
		// Upload something
		req, _ := http.NewRequest("PUT", createResp.SignedUploadURL, bytes.NewReader([]byte(key)))
		r, _ := http.DefaultClient.Do(req)
		r.Body.Close()
		// Finalize
		resp = cacheTwirpRequest(t, ts, "FinalizeCacheEntryUpload", token, map[string]any{
			"key":        key,
			"version":    "v1",
			"size_bytes": len(key),
		})
		decodeResponse[FinalizeCacheEntryResponse](t, resp)
	}

	// Lookup with restore_keys prefix
	resp := cacheTwirpRequest(t, ts, "GetCacheEntryDownloadURL", token, map[string]any{
		"key":          "node-modules-ccc",
		"version":      "v1",
		"restore_keys": []string{"node-modules-"},
		"metadata":     map[string]string{"scope": scope},
	})
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("prefix lookup status=%d body=%s", resp.StatusCode, body)
	}
	dlResp := decodeResponse[GetCacheEntryDownloadURLResponse](t, resp)
	if !dlResp.Ok || dlResp.SignedDownloadURL == "" {
		t.Fatal("prefix match should return a download URL")
	}

	// Download and verify it's one of the two entries (the newest)
	getResp, err := http.Get(dlResp.SignedDownloadURL)
	if err != nil {
		t.Fatal(err)
	}
	defer getResp.Body.Close()
	data, _ := io.ReadAll(getResp.Body)
	if string(data) != "node-modules-bbb" {
		t.Fatalf("prefix match returned %q, expected newest entry", data)
	}
}

func TestCacheOverwrite(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	token := makeTestJWT("Actions.Results:run1:job1")
	scope := "refs/heads/main"

	// Create, upload, finalize first version
	resp := cacheTwirpRequest(t, ts, "CreateCacheEntry", token, map[string]any{
		"key":      "my-cache",
		"version":  "v1",
		"metadata": map[string]string{"scope": scope},
	})
	createResp := decodeResponse[CreateCacheEntryResponse](t, resp)
	req, _ := http.NewRequest("PUT", createResp.SignedUploadURL, bytes.NewReader([]byte("old-data")))
	r, _ := http.DefaultClient.Do(req)
	r.Body.Close()
	cacheTwirpRequest(t, ts, "FinalizeCacheEntryUpload", token, map[string]any{
		"key": "my-cache", "version": "v1", "size_bytes": 8,
	})

	// Overwrite with new data
	resp = cacheTwirpRequest(t, ts, "CreateCacheEntry", token, map[string]any{
		"key":      "my-cache",
		"version":  "v1",
		"metadata": map[string]string{"scope": scope},
	})
	createResp = decodeResponse[CreateCacheEntryResponse](t, resp)
	req, _ = http.NewRequest("PUT", createResp.SignedUploadURL, bytes.NewReader([]byte("new-data")))
	r, _ = http.DefaultClient.Do(req)
	r.Body.Close()
	cacheTwirpRequest(t, ts, "FinalizeCacheEntryUpload", token, map[string]any{
		"key": "my-cache", "version": "v1", "size_bytes": 8,
	})

	// Download should return new data
	resp = cacheTwirpRequest(t, ts, "GetCacheEntryDownloadURL", token, map[string]any{
		"key":      "my-cache",
		"version":  "v1",
		"metadata": map[string]string{"scope": scope},
	})
	dlResp := decodeResponse[GetCacheEntryDownloadURLResponse](t, resp)
	getResp, err := http.Get(dlResp.SignedDownloadURL)
	if err != nil {
		t.Fatal(err)
	}
	defer getResp.Body.Close()
	data, _ := io.ReadAll(getResp.Body)
	if string(data) != "new-data" {
		t.Fatalf("overwrite failed: got %q", data)
	}
}

func TestCacheMiss(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	token := makeTestJWT("Actions.Results:run1:job1")

	resp := cacheTwirpRequest(t, ts, "GetCacheEntryDownloadURL", token, map[string]any{
		"key":      "nonexistent",
		"version":  "v1",
		"metadata": map[string]string{"scope": "refs/heads/main"},
	})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for cache miss, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestCacheInvalidAuth(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	b, _ := json.Marshal(map[string]any{"key": "k", "version": "v"})
	req, _ := http.NewRequest("POST",
		ts.URL+"/twirp/github.actions.results.api.v1.CacheService/CreateCacheEntry",
		bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	// No auth header
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestCacheSizeBytesAsString(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	token := makeTestJWT("Actions.Results:run1:job1")

	// Create cache entry
	resp := cacheTwirpRequest(t, ts, "CreateCacheEntry", token, map[string]any{
		"key":     "str-size-test",
		"version": "v1",
	})
	createResp := decodeResponse[CreateCacheEntryResponse](t, resp)
	req, _ := http.NewRequest("PUT", createResp.SignedUploadURL, bytes.NewReader([]byte("data")))
	r, _ := http.DefaultClient.Do(req)
	r.Body.Close()

	// Finalize with size_bytes as a JSON string (protobuf int64 encoding)
	b, _ := json.Marshal(map[string]any{
		"key":        "str-size-test",
		"version":    "v1",
		"size_bytes": "4",
	})
	url := ts.URL + "/twirp/github.actions.results.api.v1.CacheService/FinalizeCacheEntryUpload"
	httpReq, _ := http.NewRequest("POST", url, bytes.NewReader(b))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("FinalizeCacheEntryUpload with string size_bytes: status=%d body=%s", resp.StatusCode, body)
	}
	finalResp := decodeResponse[FinalizeCacheEntryResponse](t, resp)
	if !finalResp.Ok {
		t.Fatal("finalize failed")
	}
}

func TestCacheMatchedKey(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	token := makeTestJWT("Actions.Results:run1:job1")

	// Create, upload, finalize
	resp := cacheTwirpRequest(t, ts, "CreateCacheEntry", token, map[string]any{
		"key":     "my-key-abc",
		"version": "v1",
	})
	createResp := decodeResponse[CreateCacheEntryResponse](t, resp)
	req, _ := http.NewRequest("PUT", createResp.SignedUploadURL, bytes.NewReader([]byte("data")))
	r, _ := http.DefaultClient.Do(req)
	r.Body.Close()
	cacheTwirpRequest(t, ts, "FinalizeCacheEntryUpload", token, map[string]any{
		"key": "my-key-abc", "version": "v1", "size_bytes": 4,
	})

	// Exact match — matched_key should be the key
	resp = cacheTwirpRequest(t, ts, "GetCacheEntryDownloadURL", token, map[string]any{
		"key":     "my-key-abc",
		"version": "v1",
	})
	dlResp := decodeResponse[GetCacheEntryDownloadURLResponse](t, resp)
	if dlResp.MatchedKey != "my-key-abc" {
		t.Fatalf("exact match: matched_key=%q, want %q", dlResp.MatchedKey, "my-key-abc")
	}

	// Prefix match via restore_keys — matched_key should be the matched entry's key
	resp = cacheTwirpRequest(t, ts, "GetCacheEntryDownloadURL", token, map[string]any{
		"key":          "my-key-xyz",
		"version":      "v1",
		"restore_keys": []string{"my-key-"},
	})
	dlResp = decodeResponse[GetCacheEntryDownloadURLResponse](t, resp)
	if dlResp.MatchedKey != "my-key-abc" {
		t.Fatalf("prefix match: matched_key=%q, want %q", dlResp.MatchedKey, "my-key-abc")
	}
}

func TestCacheInvalidContentType(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	req, _ := http.NewRequest("POST",
		ts.URL+"/twirp/github.actions.results.api.v1.CacheService/CreateCacheEntry",
		strings.NewReader("{}"))
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("Actions.Results:run1:job1"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}
