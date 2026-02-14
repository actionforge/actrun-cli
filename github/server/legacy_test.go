package server

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func legacyRequest(t *testing.T, ts *httptest.Server, method, path string, body any) *http.Response {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		bodyReader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, ts.URL+path, bodyReader)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer test-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

func TestLegacyFullCycle(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	fileContent := []byte("hello legacy artifact")

	// 1. Create container
	resp := legacyRequest(t, ts, "POST", "/_apis/pipelines/workflows/run1/artifacts",
		map[string]string{"name": "my-artifact", "type": "actions_storage"})
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("create status=%d body=%s", resp.StatusCode, body)
	}
	var createResult map[string]string
	json.NewDecoder(resp.Body).Decode(&createResult)
	resp.Body.Close()
	uploadURL := createResult["fileContainerResourceUrl"]
	if uploadURL == "" {
		t.Fatal("missing fileContainerResourceUrl")
	}

	// 2. Upload file
	req, _ := http.NewRequest("PUT", uploadURL+"?itemPath=data/file.txt", bytes.NewReader(fileContent))
	req.Header.Set("Content-Type", "application/octet-stream")
	uploadResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	uploadResp.Body.Close()
	if uploadResp.StatusCode != http.StatusCreated {
		t.Fatalf("upload status=%d", uploadResp.StatusCode)
	}

	// 3. Finalize
	resp = legacyRequest(t, ts, "PATCH", "/_apis/pipelines/workflows/run1/artifacts?artifactName=my-artifact", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("finalize status=%d", resp.StatusCode)
	}
	resp.Body.Close()

	// 4. List containers
	resp = legacyRequest(t, ts, "GET", "/_apis/pipelines/workflows/run1/artifacts", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status=%d", resp.StatusCode)
	}
	var listResult struct {
		Count int              `json:"count"`
		Value []map[string]any `json:"value"`
	}
	json.NewDecoder(resp.Body).Decode(&listResult)
	resp.Body.Close()
	if listResult.Count != 1 {
		t.Fatalf("expected 1 container, got %d", listResult.Count)
	}
	if listResult.Value[0]["name"] != "my-artifact" {
		t.Fatalf("name=%v", listResult.Value[0]["name"])
	}
	downloadListURL := listResult.Value[0]["fileContainerResourceUrl"].(string)

	// 5. List files in container
	resp = legacyRequest(t, ts, "GET", extractPath(downloadListURL), nil)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("list files status=%d body=%s", resp.StatusCode, body)
	}
	var filesResult struct {
		Value []map[string]any `json:"value"`
	}
	json.NewDecoder(resp.Body).Decode(&filesResult)
	resp.Body.Close()
	if len(filesResult.Value) != 1 {
		t.Fatalf("expected 1 file, got %d", len(filesResult.Value))
	}
	if filesResult.Value[0]["path"] != "data/file.txt" {
		t.Fatalf("path=%v", filesResult.Value[0]["path"])
	}
	contentLocation := filesResult.Value[0]["contentLocation"].(string)

	// 6. Download file
	dlResp, err := http.Get(contentLocation)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	defer dlResp.Body.Close()
	if dlResp.StatusCode != http.StatusOK {
		t.Fatalf("download status=%d", dlResp.StatusCode)
	}
	data, _ := io.ReadAll(dlResp.Body)
	if !bytes.Equal(data, fileContent) {
		t.Fatalf("content mismatch: got %q", data)
	}
}

func TestLegacyChunkedUpload(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	// Create container
	resp := legacyRequest(t, ts, "POST", "/_apis/pipelines/workflows/run2/artifacts",
		map[string]string{"name": "chunked", "type": "actions_storage"})
	var createResult map[string]string
	json.NewDecoder(resp.Body).Decode(&createResult)
	resp.Body.Close()
	uploadURL := createResult["fileContainerResourceUrl"]

	chunk1 := []byte("AAAA")
	chunk2 := []byte("BBBB")
	total := len(chunk1) + len(chunk2)

	// Upload chunk 1
	req, _ := http.NewRequest("PUT", uploadURL+"?itemPath=file.bin", bytes.NewReader(chunk1))
	req.Header.Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", len(chunk1)-1, total))
	r, _ := http.DefaultClient.Do(req)
	r.Body.Close()
	if r.StatusCode != http.StatusCreated {
		t.Fatalf("chunk1 status=%d", r.StatusCode)
	}

	// Upload chunk 2
	req, _ = http.NewRequest("PUT", uploadURL+"?itemPath=file.bin", bytes.NewReader(chunk2))
	req.Header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", len(chunk1), total-1, total))
	r, _ = http.DefaultClient.Do(req)
	r.Body.Close()
	if r.StatusCode != http.StatusCreated {
		t.Fatalf("chunk2 status=%d", r.StatusCode)
	}

	// Finalize
	resp = legacyRequest(t, ts, "PATCH", "/_apis/pipelines/workflows/run2/artifacts?artifactName=chunked", nil)
	resp.Body.Close()

	// List files and download
	resp = legacyRequest(t, ts, "GET", "/_apis/pipelines/workflows/run2/artifacts", nil)
	var listResult struct {
		Value []map[string]any `json:"value"`
	}
	json.NewDecoder(resp.Body).Decode(&listResult)
	resp.Body.Close()
	downloadListURL := listResult.Value[0]["fileContainerResourceUrl"].(string)

	resp = legacyRequest(t, ts, "GET", extractPath(downloadListURL), nil)
	var filesResult struct {
		Value []map[string]any `json:"value"`
	}
	json.NewDecoder(resp.Body).Decode(&filesResult)
	resp.Body.Close()
	contentLocation := filesResult.Value[0]["contentLocation"].(string)

	dlResp, _ := http.Get(contentLocation)
	defer dlResp.Body.Close()
	data, _ := io.ReadAll(dlResp.Body)
	expected := append(chunk1, chunk2...)
	if !bytes.Equal(data, expected) {
		t.Fatalf("chunked content mismatch: got %q, want %q", data, expected)
	}
}

func TestLegacyGzipRoundtrip(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	// Create container
	resp := legacyRequest(t, ts, "POST", "/_apis/pipelines/workflows/run3/artifacts",
		map[string]string{"name": "gzipped", "type": "actions_storage"})
	var createResult map[string]string
	json.NewDecoder(resp.Body).Decode(&createResult)
	resp.Body.Close()
	uploadURL := createResult["fileContainerResourceUrl"]

	// Gzip some data
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	gw.Write([]byte("compressed content"))
	gw.Close()
	gzipData := buf.Bytes()

	// Upload with Content-Encoding: gzip
	req, _ := http.NewRequest("PUT", uploadURL+"?itemPath=data.gz", bytes.NewReader(gzipData))
	req.Header.Set("Content-Encoding", "gzip")
	r, _ := http.DefaultClient.Do(req)
	r.Body.Close()
	if r.StatusCode != http.StatusCreated {
		t.Fatalf("upload status=%d", r.StatusCode)
	}

	// Finalize
	resp = legacyRequest(t, ts, "PATCH", "/_apis/pipelines/workflows/run3/artifacts?artifactName=gzipped", nil)
	resp.Body.Close()

	// List and download
	resp = legacyRequest(t, ts, "GET", "/_apis/pipelines/workflows/run3/artifacts", nil)
	var listResult struct{ Value []map[string]any }
	json.NewDecoder(resp.Body).Decode(&listResult)
	resp.Body.Close()

	resp = legacyRequest(t, ts, "GET", extractPath(listResult.Value[0]["fileContainerResourceUrl"].(string)), nil)
	var filesResult struct{ Value []map[string]any }
	json.NewDecoder(resp.Body).Decode(&filesResult)
	resp.Body.Close()

	// Use raw HTTP transport to avoid automatic decompression
	transport := &http.Transport{DisableCompression: true}
	client := &http.Client{Transport: transport}
	dlResp, _ := client.Get(filesResult.Value[0]["contentLocation"].(string))
	defer dlResp.Body.Close()

	if dlResp.Header.Get("Content-Encoding") != "gzip" {
		t.Fatal("expected Content-Encoding: gzip on download")
	}

	rawData, _ := io.ReadAll(dlResp.Body)
	if !bytes.Equal(rawData, gzipData) {
		t.Fatalf("gzip data mismatch: got %d bytes, want %d bytes", len(rawData), len(gzipData))
	}
}

func TestLegacyMultipleFiles(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	// Create container
	resp := legacyRequest(t, ts, "POST", "/_apis/pipelines/workflows/run4/artifacts",
		map[string]string{"name": "multi", "type": "actions_storage"})
	var createResult map[string]string
	json.NewDecoder(resp.Body).Decode(&createResult)
	resp.Body.Close()
	uploadURL := createResult["fileContainerResourceUrl"]

	files := map[string]string{
		"dir/a.txt": "content-a",
		"dir/b.txt": "content-b",
		"c.txt":     "content-c",
	}

	for path, content := range files {
		req, _ := http.NewRequest("PUT", uploadURL+"?itemPath="+path, bytes.NewReader([]byte(content)))
		r, _ := http.DefaultClient.Do(req)
		r.Body.Close()
	}

	// Finalize
	resp = legacyRequest(t, ts, "PATCH", "/_apis/pipelines/workflows/run4/artifacts?artifactName=multi", nil)
	resp.Body.Close()

	// List files
	resp = legacyRequest(t, ts, "GET", "/_apis/pipelines/workflows/run4/artifacts", nil)
	var listResult struct{ Value []map[string]any }
	json.NewDecoder(resp.Body).Decode(&listResult)
	resp.Body.Close()

	resp = legacyRequest(t, ts, "GET", extractPath(listResult.Value[0]["fileContainerResourceUrl"].(string)), nil)
	var filesResult struct{ Value []map[string]any }
	json.NewDecoder(resp.Body).Decode(&filesResult)
	resp.Body.Close()
	if len(filesResult.Value) != 3 {
		t.Fatalf("expected 3 files, got %d", len(filesResult.Value))
	}

	// Filter by prefix
	resp = legacyRequest(t, ts, "GET", extractPath(listResult.Value[0]["fileContainerResourceUrl"].(string))+"?itemPath=dir/", nil)
	json.NewDecoder(resp.Body).Decode(&filesResult)
	resp.Body.Close()
	if len(filesResult.Value) != 2 {
		t.Fatalf("expected 2 files with prefix dir/, got %d", len(filesResult.Value))
	}
}

func TestLegacyNotFound(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	// List containers for non-existent run
	resp := legacyRequest(t, ts, "GET", "/_apis/pipelines/workflows/norun/artifacts", nil)
	var listResult struct {
		Count int `json:"count"`
	}
	json.NewDecoder(resp.Body).Decode(&listResult)
	resp.Body.Close()
	if listResult.Count != 0 {
		t.Fatalf("expected 0 containers, got %d", listResult.Count)
	}

	// Download non-existent container
	dlResp, _ := http.Get(ts.URL + "/download-v3/9999")
	dlResp.Body.Close()
	if dlResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", dlResp.StatusCode)
	}

	// Download non-existent file
	dlResp, _ = http.Get(ts.URL + "/artifact/9999/nofile.txt")
	dlResp.Body.Close()
	if dlResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", dlResp.StatusCode)
	}
}

func TestLegacyUnauthorized(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	req, _ := http.NewRequest("POST", ts.URL+"/_apis/pipelines/workflows/run1/artifacts",
		bytes.NewReader([]byte(`{"name":"test"}`)))
	req.Header.Set("Content-Type", "application/json")
	// No auth header
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// extractPath extracts the path from a full URL for use with legacyRequest.
func extractPath(fullURL string) string {
	// Find path after the host
	if idx := len("http://"); idx < len(fullURL) {
		rest := fullURL[idx:]
		if slashIdx := slashIndex(rest); slashIdx >= 0 {
			return rest[slashIdx:]
		}
	}
	return fullURL
}

func slashIndex(s string) int {
	for i, c := range s {
		if c == '/' {
			return i
		}
	}
	return -1
}
