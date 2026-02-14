package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type OIDCConfig struct {
	Issuer  string
	Subject string
}

func (s *Server) handleOIDCToken(w http.ResponseWriter, r *http.Request) {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") || strings.TrimPrefix(auth, "Bearer ") == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"unauthorized"}`))
		return
	}

	audience := r.URL.Query().Get("audience")
	if audience == "" {
		audience = s.externalURL
	}

	now := time.Now()
	sub := s.oidcCfg.Subject
	ref := "refs/heads/main"
	repository := "local/repo"
	actor := "local-runner"

	// Parse subject: "repo:org/repo:ref:refs/heads/branch"
	if strings.HasPrefix(sub, "repo:") {
		rest := strings.TrimPrefix(sub, "repo:")
		if idx := strings.Index(rest, ":ref:"); idx >= 0 {
			repository = rest[:idx]
			ref = rest[idx+5:]
		} else if idx := strings.Index(rest, ":"); idx >= 0 {
			repository = rest[:idx]
		}
	}

	claims := map[string]any{
		"iss":        s.oidcCfg.Issuer,
		"sub":        sub,
		"aud":        audience,
		"iat":        now.Unix(),
		"exp":        now.Add(1 * time.Hour).Unix(),
		"ref":        ref,
		"sha":        "local",
		"repository": repository,
		"actor":      actor,
		"run_id":     "1",
		"run_number": "1",
		"workflow":   "local",
		"event_name": "push",
	}

	jwt, err := mintJWT(s.signingKey, claims)
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"value": jwt})
}

func mintJWT(key []byte, claims map[string]any) (string, error) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))

	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(claimsJSON)

	sigInput := header + "." + payload
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(sigInput))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return sigInput + "." + sig, nil
}
