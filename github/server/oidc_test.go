package server

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestOIDCTokenCustomAudience(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	req, _ := http.NewRequest("GET", ts.URL+"/_services/token?audience=sts.amazonaws.com", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	jwt := result["value"]
	if jwt == "" {
		t.Fatal("missing JWT value")
	}

	claims := decodeJWTClaims(t, jwt)
	if claims["aud"] != "sts.amazonaws.com" {
		t.Fatalf("aud=%v, want sts.amazonaws.com", claims["aud"])
	}
	if claims["iss"] != "https://token.actions.githubusercontent.com" {
		t.Fatalf("iss=%v", claims["iss"])
	}
	if claims["sub"] != "repo:local/repo:ref:refs/heads/main" {
		t.Fatalf("sub=%v", claims["sub"])
	}
	if claims["repository"] != "local/repo" {
		t.Fatalf("repository=%v", claims["repository"])
	}
	if claims["ref"] != "refs/heads/main" {
		t.Fatalf("ref=%v", claims["ref"])
	}
	if claims["actor"] != "local-runner" {
		t.Fatalf("actor=%v", claims["actor"])
	}
	if claims["event_name"] != "push" {
		t.Fatalf("event_name=%v", claims["event_name"])
	}
}

func TestOIDCTokenDefaultAudience(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	req, _ := http.NewRequest("GET", ts.URL+"/_services/token", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	claims := decodeJWTClaims(t, result["value"])

	// Default audience should be the server's external URL
	if !strings.HasPrefix(claims["aud"].(string), "http://") {
		t.Fatalf("default aud=%v, expected server URL", claims["aud"])
	}
}

func TestOIDCTokenMissingAuth(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/_services/token")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestOIDCTokenEmptyBearer(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	req, _ := http.NewRequest("GET", ts.URL+"/_services/token", nil)
	req.Header.Set("Authorization", "Bearer ")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestOIDCJWTStructure(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	req, _ := http.NewRequest("GET", ts.URL+"/_services/token?audience=test", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	jwt := result["value"]

	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		t.Fatalf("JWT should have 3 parts, got %d", len(parts))
	}

	// Decode header
	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("decode header: %v", err)
	}
	var header map[string]string
	json.Unmarshal(headerJSON, &header)
	if header["alg"] != "HS256" {
		t.Fatalf("alg=%s, want HS256", header["alg"])
	}
	if header["typ"] != "JWT" {
		t.Fatalf("typ=%s, want JWT", header["typ"])
	}

	// Verify all expected claims exist
	claims := decodeJWTClaims(t, jwt)
	for _, key := range []string{"iss", "sub", "aud", "iat", "exp", "ref", "sha", "repository", "actor", "run_id", "run_number", "workflow", "event_name"} {
		if _, ok := claims[key]; !ok {
			t.Errorf("missing claim: %s", key)
		}
	}
}

func decodeJWTClaims(t *testing.T, jwt string) map[string]any {
	t.Helper()
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		t.Fatalf("invalid JWT: %d parts", len(parts))
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatalf("unmarshal claims: %v", err)
	}
	return claims
}
