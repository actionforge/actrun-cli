package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func makeTestJWT(scp string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"scp":"%s"}`, scp)))
	return header + "." + payload + ".sig"
}

func setupTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	dir := t.TempDir()
	signingKey := []byte("test-signing-key-32-bytes-long!!")
	ts := httptest.NewServer(nil)
	srv := NewServer(dir, signingKey, ts.URL, OIDCConfig{
		Issuer:  "https://token.actions.githubusercontent.com",
		Subject: "repo:local/repo:ref:refs/heads/main",
	})
	ts.Config.Handler = srv
	return srv, ts
}

func twirpRequest(t *testing.T, ts *httptest.Server, method string, token string, body interface{}) *http.Response {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	url := ts.URL + "/twirp/github.actions.results.api.v1.ArtifactService/" + method
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

func decodeResponse[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	defer resp.Body.Close()
	var v T
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return v
}

// --- Full upload/download/delete cycle ---

func TestFullCycle(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	token := makeTestJWT("Actions.Results:run1:job1")
	blobContent := []byte("hello artifact world")

	// 1. CreateArtifact
	resp := twirpRequest(t, ts, "CreateArtifact", token, map[string]interface{}{
		"workflow_run_backend_id":     "run1",
		"workflow_job_run_backend_id": "job1",
		"name":                        "my-artifact",
		"version":                     4,
	})
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("CreateArtifact status=%d body=%s", resp.StatusCode, body)
	}
	createResp := decodeResponse[CreateArtifactResponse](t, resp)
	if !createResp.Ok {
		t.Fatal("CreateArtifact ok=false")
	}
	if createResp.SignedUploadURL == "" {
		t.Fatal("CreateArtifact missing signed_upload_url")
	}

	// 2. Upload blob (simple PUT)
	req, _ := http.NewRequest("PUT", createResp.SignedUploadURL, bytes.NewReader(blobContent))
	uploadResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("upload blob: %v", err)
	}
	uploadResp.Body.Close()
	if uploadResp.StatusCode != http.StatusCreated {
		t.Fatalf("upload blob status=%d", uploadResp.StatusCode)
	}

	// 3. FinalizeArtifact
	resp = twirpRequest(t, ts, "FinalizeArtifact", token, map[string]interface{}{
		"workflow_run_backend_id":     "run1",
		"workflow_job_run_backend_id": "job1",
		"name":                        "my-artifact",
		"size":                        strconv.Itoa(len(blobContent)),
	})
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("FinalizeArtifact status=%d body=%s", resp.StatusCode, body)
	}
	finalizeResp := decodeResponse[FinalizeArtifactResponse](t, resp)
	if !finalizeResp.Ok {
		t.Fatal("FinalizeArtifact ok=false")
	}
	if finalizeResp.ArtifactID == "" {
		t.Fatal("FinalizeArtifact missing artifact_id")
	}

	// 4. ListArtifacts
	resp = twirpRequest(t, ts, "ListArtifacts", token, map[string]interface{}{
		"workflow_run_backend_id": "run1",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ListArtifacts status=%d", resp.StatusCode)
	}
	listResp := decodeResponse[ListArtifactsResponse](t, resp)
	if len(listResp.Artifacts) != 1 {
		t.Fatalf("ListArtifacts expected 1, got %d", len(listResp.Artifacts))
	}
	if listResp.Artifacts[0].Name != "my-artifact" {
		t.Fatalf("ListArtifacts name=%q", listResp.Artifacts[0].Name)
	}
	if listResp.Artifacts[0].Size != strconv.Itoa(len(blobContent)) {
		t.Fatalf("ListArtifacts size=%q", listResp.Artifacts[0].Size)
	}

	// 5. GetSignedArtifactURL
	resp = twirpRequest(t, ts, "GetSignedArtifactURL", token, map[string]interface{}{
		"workflow_run_backend_id": "run1",
		"name":                   "my-artifact",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GetSignedArtifactURL status=%d", resp.StatusCode)
	}
	urlResp := decodeResponse[GetSignedArtifactURLResponse](t, resp)
	if urlResp.SignedURL == "" {
		t.Fatal("GetSignedArtifactURL missing signed_url")
	}

	// 6. Download blob
	dlResp, err := http.Get(urlResp.SignedURL)
	if err != nil {
		t.Fatalf("download blob: %v", err)
	}
	defer dlResp.Body.Close()
	if dlResp.StatusCode != http.StatusOK {
		t.Fatalf("download blob status=%d", dlResp.StatusCode)
	}
	downloaded, _ := io.ReadAll(dlResp.Body)
	if !bytes.Equal(downloaded, blobContent) {
		t.Fatalf("downloaded content mismatch: got %q", downloaded)
	}

	// 7. DeleteArtifact
	resp = twirpRequest(t, ts, "DeleteArtifact", token, map[string]interface{}{
		"workflow_run_backend_id": "run1",
		"name":                   "my-artifact",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("DeleteArtifact status=%d", resp.StatusCode)
	}
	deleteResp := decodeResponse[DeleteArtifactResponse](t, resp)
	if !deleteResp.Ok {
		t.Fatal("DeleteArtifact ok=false")
	}

	// Verify artifact is gone
	resp = twirpRequest(t, ts, "ListArtifacts", token, map[string]interface{}{
		"workflow_run_backend_id": "run1",
	})
	listResp = decodeResponse[ListArtifactsResponse](t, resp)
	if len(listResp.Artifacts) != 0 {
		t.Fatalf("expected 0 artifacts after delete, got %d", len(listResp.Artifacts))
	}
}

// --- Chunked upload (comp=block + comp=blocklist) ---

func TestChunkedUpload(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	token := makeTestJWT("Actions.Results:run2:job2")

	// Create artifact
	resp := twirpRequest(t, ts, "CreateArtifact", token, map[string]interface{}{
		"workflow_run_backend_id":     "run2",
		"workflow_job_run_backend_id": "job2",
		"name":                        "chunked-artifact",
		"version":                     4,
	})
	createResp := decodeResponse[CreateArtifactResponse](t, resp)
	if !createResp.Ok {
		t.Fatal("CreateArtifact ok=false")
	}

	uploadURL := createResp.SignedUploadURL
	chunk1 := []byte("chunk-one-")
	chunk2 := []byte("chunk-two-")
	chunk3 := []byte("chunk-three")

	// Upload blocks
	for i, chunk := range [][]byte{chunk1, chunk2, chunk3} {
		blockID := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("block%d", i)))
		u := uploadURL + "&comp=block&blockid=" + url.QueryEscape(blockID)
		req, _ := http.NewRequest("PUT", u, bytes.NewReader(chunk))
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("upload block %d: %v", i, err)
		}
		r.Body.Close()
		if r.StatusCode != http.StatusCreated {
			t.Fatalf("upload block %d status=%d", i, r.StatusCode)
		}
	}

	// Commit block list
	blockListXML := `<?xml version="1.0" encoding="utf-8"?><BlockList><Latest>YmxvY2sw</Latest><Latest>YmxvY2sx</Latest><Latest>YmxvY2sy</Latest></BlockList>`
	commitURL := uploadURL + "&comp=blocklist"
	req, _ := http.NewRequest("PUT", commitURL, strings.NewReader(blockListXML))
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("commit blocklist: %v", err)
	}
	r.Body.Close()
	if r.StatusCode != http.StatusCreated {
		t.Fatalf("commit blocklist status=%d", r.StatusCode)
	}

	// Finalize
	totalSize := len(chunk1) + len(chunk2) + len(chunk3)
	resp = twirpRequest(t, ts, "FinalizeArtifact", token, map[string]interface{}{
		"workflow_run_backend_id":     "run2",
		"workflow_job_run_backend_id": "job2",
		"name":                        "chunked-artifact",
		"size":                        strconv.Itoa(totalSize),
	})
	finalizeResp := decodeResponse[FinalizeArtifactResponse](t, resp)
	if !finalizeResp.Ok {
		t.Fatal("FinalizeArtifact ok=false")
	}

	// Download and verify
	resp = twirpRequest(t, ts, "GetSignedArtifactURL", token, map[string]interface{}{
		"workflow_run_backend_id": "run2",
		"name":                   "chunked-artifact",
	})
	urlResp := decodeResponse[GetSignedArtifactURLResponse](t, resp)
	dlResp, _ := http.Get(urlResp.SignedURL)
	defer dlResp.Body.Close()
	data, _ := io.ReadAll(dlResp.Body)
	expected := append(append(chunk1, chunk2...), chunk3...)
	if !bytes.Equal(data, expected) {
		t.Fatalf("chunked data mismatch: got %q, want %q", data, expected)
	}
}

// --- Signed URL expiry ---

func TestSignedURLExpiry(t *testing.T) {
	srv, ts := setupTestServer(t)
	defer ts.Close()

	// Create an expired signed URL manually
	expiredSig := srv.computeSignature("PUT", 999, time.Now().Add(-1*time.Hour).Unix())
	expiredURL := fmt.Sprintf("%s/upload/999?sig=%s&exp=%d", ts.URL, expiredSig, time.Now().Add(-1*time.Hour).Unix())

	req, _ := http.NewRequest("PUT", expiredURL, strings.NewReader("data"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for expired URL, got %d", resp.StatusCode)
	}
}

func TestSignedURLInvalidSignature(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	badURL := fmt.Sprintf("%s/upload/1?sig=badsig&exp=%d", ts.URL, time.Now().Add(1*time.Hour).Unix())
	req, _ := http.NewRequest("PUT", badURL, strings.NewReader("data"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for invalid sig, got %d", resp.StatusCode)
	}
}

func TestSignedURLMissingParams(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	req, _ := http.NewRequest("PUT", ts.URL+"/upload/1", strings.NewReader("data"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for missing params, got %d", resp.StatusCode)
	}
}

// --- JWT parsing ---

func TestParseJWT(t *testing.T) {
	tests := []struct {
		name      string
		auth      string
		wantRun   string
		wantJob   string
		wantErr   bool
	}{
		{
			name:    "valid JWT",
			auth:    "Bearer " + makeTestJWT("Actions.Results:run1:job1"),
			wantRun: "run1",
			wantJob: "job1",
		},
		{
			name:    "multiple scopes",
			auth:    "Bearer " + makeTestJWT("Actions.Read Actions.Results:runX:jobY Actions.Write"),
			wantRun: "runX",
			wantJob: "jobY",
		},
		{
			name:    "missing auth",
			auth:    "",
			wantErr: true,
		},
		{
			name:    "no Bearer prefix",
			auth:    "Token abc",
			wantErr: true,
		},
		{
			name:    "invalid JWT structure",
			auth:    "Bearer not.a.valid-base64!",
			wantErr: true,
		},
		{
			name:    "JWT without scope",
			auth:    "Bearer " + makeTestJWT("Actions.Read"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runID, jobID, err := parseJWT(tt.auth)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if runID != tt.wantRun {
				t.Fatalf("runID=%q, want %q", runID, tt.wantRun)
			}
			if jobID != tt.wantJob {
				t.Fatalf("jobID=%q, want %q", jobID, tt.wantJob)
			}
		})
	}
}

// --- Error responses ---

func TestErrorNotFound(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	token := makeTestJWT("Actions.Results:run1:job1")

	// FinalizeArtifact for nonexistent artifact
	resp := twirpRequest(t, ts, "FinalizeArtifact", token, map[string]interface{}{
		"workflow_run_backend_id":     "run1",
		"workflow_job_run_backend_id": "job1",
		"name":                        "nonexistent",
		"size":                        "0",
	})
	if resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("expected 404, got %d: %s", resp.StatusCode, body)
	}
	resp.Body.Close()

	// GetSignedArtifactURL for nonexistent
	resp = twirpRequest(t, ts, "GetSignedArtifactURL", token, map[string]interface{}{
		"workflow_run_backend_id": "run1",
		"name":                   "nonexistent",
	})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// DeleteArtifact for nonexistent
	resp = twirpRequest(t, ts, "DeleteArtifact", token, map[string]interface{}{
		"workflow_run_backend_id": "run1",
		"name":                   "nonexistent",
	})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestDuplicateArtifact(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	token := makeTestJWT("Actions.Results:run1:job1")

	// Create and finalize
	resp := twirpRequest(t, ts, "CreateArtifact", token, map[string]interface{}{
		"workflow_run_backend_id":     "run1",
		"workflow_job_run_backend_id": "job1",
		"name":                        "dup-artifact",
		"version":                     4,
	})
	createResp := decodeResponse[CreateArtifactResponse](t, resp)
	if !createResp.Ok {
		t.Fatal("first CreateArtifact failed")
	}

	resp = twirpRequest(t, ts, "FinalizeArtifact", token, map[string]interface{}{
		"workflow_run_backend_id":     "run1",
		"workflow_job_run_backend_id": "job1",
		"name":                        "dup-artifact",
		"size":                        "0",
	})
	finalizeResp := decodeResponse[FinalizeArtifactResponse](t, resp)
	if !finalizeResp.Ok {
		t.Fatal("FinalizeArtifact failed")
	}

	// Try to create again -> should fail
	resp = twirpRequest(t, ts, "CreateArtifact", token, map[string]interface{}{
		"workflow_run_backend_id":     "run1",
		"workflow_job_run_backend_id": "job1",
		"name":                        "dup-artifact",
		"version":                     4,
	})
	if resp.StatusCode != http.StatusConflict {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("expected 409 for duplicate, got %d: %s", resp.StatusCode, body)
	}
	resp.Body.Close()
}

func TestInvalidAuth(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	// No auth header
	b, _ := json.Marshal(map[string]interface{}{
		"workflow_run_backend_id": "run1",
	})
	req, _ := http.NewRequest("POST",
		ts.URL+"/twirp/github.actions.results.api.v1.ArtifactService/ListArtifacts",
		bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestInvalidContentType(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	req, _ := http.NewRequest("POST",
		ts.URL+"/twirp/github.actions.results.api.v1.ArtifactService/ListArtifacts",
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

func TestUnknownMethod(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	token := makeTestJWT("Actions.Results:run1:job1")
	resp := twirpRequest(t, ts, "UnknownMethod", token, map[string]string{})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

// --- ListArtifacts filtering ---

func TestListArtifactsFilters(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	token := makeTestJWT("Actions.Results:runF:jobF")

	// Create and finalize two artifacts
	for _, name := range []string{"alpha", "beta"} {
		resp := twirpRequest(t, ts, "CreateArtifact", token, map[string]interface{}{
			"workflow_run_backend_id":     "runF",
			"workflow_job_run_backend_id": "jobF",
			"name":                        name,
			"version":                     4,
		})
		decodeResponse[CreateArtifactResponse](t, resp)

		resp = twirpRequest(t, ts, "FinalizeArtifact", token, map[string]interface{}{
			"workflow_run_backend_id":     "runF",
			"workflow_job_run_backend_id": "jobF",
			"name":                        name,
			"size":                        "100",
		})
		decodeResponse[FinalizeArtifactResponse](t, resp)
	}

	// Filter by name
	resp := twirpRequest(t, ts, "ListArtifacts", token, map[string]interface{}{
		"workflow_run_backend_id": "runF",
		"name_filter":            "alpha",
	})
	listResp := decodeResponse[ListArtifactsResponse](t, resp)
	if len(listResp.Artifacts) != 1 || listResp.Artifacts[0].Name != "alpha" {
		t.Fatalf("name filter: got %+v", listResp.Artifacts)
	}

	// Unfinalized artifacts should not appear
	resp = twirpRequest(t, ts, "CreateArtifact", token, map[string]interface{}{
		"workflow_run_backend_id":     "runF",
		"workflow_job_run_backend_id": "jobF",
		"name":                        "gamma",
		"version":                     4,
	})
	decodeResponse[CreateArtifactResponse](t, resp)

	resp = twirpRequest(t, ts, "ListArtifacts", token, map[string]interface{}{
		"workflow_run_backend_id": "runF",
	})
	listResp = decodeResponse[ListArtifactsResponse](t, resp)
	if len(listResp.Artifacts) != 2 {
		t.Fatalf("expected 2 finalized artifacts, got %d", len(listResp.Artifacts))
	}
}

// --- Migrate artifact flow ---

func TestMigrateArtifact(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	token := makeTestJWT("Actions.Results:runM:jobM")
	blobContent := []byte("migrated data")

	// MigrateArtifact
	resp := twirpRequest(t, ts, "MigrateArtifact", token, map[string]interface{}{
		"workflow_run_backend_id": "runM",
		"name":                   "migrated-artifact",
		"version":                4,
	})
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("MigrateArtifact status=%d body=%s", resp.StatusCode, body)
	}
	migrateResp := decodeResponse[MigrateArtifactResponse](t, resp)
	if !migrateResp.Ok {
		t.Fatal("MigrateArtifact ok=false")
	}

	// Upload blob
	req, _ := http.NewRequest("PUT", migrateResp.SignedUploadURL, bytes.NewReader(blobContent))
	uploadResp, _ := http.DefaultClient.Do(req)
	uploadResp.Body.Close()
	if uploadResp.StatusCode != http.StatusCreated {
		t.Fatalf("upload status=%d", uploadResp.StatusCode)
	}

	// FinalizeMigratedArtifact
	resp = twirpRequest(t, ts, "FinalizeMigratedArtifact", token, map[string]interface{}{
		"workflow_run_backend_id": "runM",
		"name":                   "migrated-artifact",
		"size":                   strconv.Itoa(len(blobContent)),
	})
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("FinalizeMigratedArtifact status=%d body=%s", resp.StatusCode, body)
	}
	finalResp := decodeResponse[FinalizeMigratedArtifactResponse](t, resp)
	if !finalResp.Ok {
		t.Fatal("FinalizeMigratedArtifact ok=false")
	}

	// Download and verify
	resp = twirpRequest(t, ts, "GetSignedArtifactURL", token, map[string]interface{}{
		"workflow_run_backend_id": "runM",
		"name":                   "migrated-artifact",
	})
	urlResp := decodeResponse[GetSignedArtifactURLResponse](t, resp)
	dlResp, _ := http.Get(urlResp.SignedURL)
	defer dlResp.Body.Close()
	data, _ := io.ReadAll(dlResp.Body)
	if !bytes.Equal(data, blobContent) {
		t.Fatalf("migrated content mismatch: got %q", data)
	}
}

// --- Download nonexistent blob ---

func TestDownloadNonexistentBlob(t *testing.T) {
	srv, ts := setupTestServer(t)
	defer ts.Close()

	// Craft a valid signed URL for a nonexistent blob
	exp := time.Now().Add(1 * time.Hour).Unix()
	sig := srv.computeSignature("GET", 999, exp)
	dlURL := fmt.Sprintf("%s/download/999?sig=%s&exp=%d", ts.URL, sig, exp)
	resp, _ := http.Get(dlURL)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for nonexistent blob, got %d", resp.StatusCode)
	}
}

// --- Blob deletion removes file ---

func TestDeleteRemovesBlobFile(t *testing.T) {
	srv, ts := setupTestServer(t)
	defer ts.Close()

	token := makeTestJWT("Actions.Results:runD:jobD")
	blobContent := []byte("to be deleted")

	// Create + upload + finalize
	resp := twirpRequest(t, ts, "CreateArtifact", token, map[string]interface{}{
		"workflow_run_backend_id":     "runD",
		"workflow_job_run_backend_id": "jobD",
		"name":                        "del-artifact",
		"version":                     4,
	})
	createResp := decodeResponse[CreateArtifactResponse](t, resp)
	req, _ := http.NewRequest("PUT", createResp.SignedUploadURL, bytes.NewReader(blobContent))
	r, _ := http.DefaultClient.Do(req)
	r.Body.Close()

	resp = twirpRequest(t, ts, "FinalizeArtifact", token, map[string]interface{}{
		"workflow_run_backend_id":     "runD",
		"workflow_job_run_backend_id": "jobD",
		"name":                        "del-artifact",
		"size":                        strconv.Itoa(len(blobContent)),
	})
	decodeResponse[FinalizeArtifactResponse](t, resp)

	// Check blob file exists
	blobPath := filepath.Join(srv.storageDir, "runD", "del-artifact")
	if _, err := os.Stat(blobPath); err != nil {
		t.Fatalf("blob file should exist: %v", err)
	}

	// Delete
	resp = twirpRequest(t, ts, "DeleteArtifact", token, map[string]interface{}{
		"workflow_run_backend_id": "runD",
		"name":                   "del-artifact",
	})
	decodeResponse[DeleteArtifactResponse](t, resp)

	// Check blob file is gone
	if _, err := os.Stat(blobPath); !os.IsNotExist(err) {
		t.Fatal("blob file should be deleted")
	}
}
