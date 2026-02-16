package server

import (
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
)

// Config holds parameters for starting a local GitHub Actions server.
type Config struct {
	StorageDir string // Directory for blob storage (created if missing)
	OIDCIssuer string // OIDC token issuer (defaults to GitHub's)
	OIDCSub    string // OIDC token subject (defaults to repo:local/repo:ref:refs/heads/main)
}

// RunningServer represents a started server instance.
type RunningServer struct {
	URL          string // Base URL of the server (e.g. http://127.0.0.1:12345)
	RuntimeToken string // JWT token for ACTIONS_RUNTIME_TOKEN
	httpServer   *http.Server
}

// Stop gracefully shuts down the server.
func (rs *RunningServer) Stop() {
	rs.httpServer.Close()
}

// InjectEnv sets GitHub Actions environment variables into the given env map.
func (rs *RunningServer) InjectEnv(env map[string]string) {
	env["ACTIONS_RUNTIME_URL"] = rs.URL + "/"
	env["ACTIONS_RUNTIME_TOKEN"] = rs.RuntimeToken
	env["ACTIONS_CACHE_URL"] = rs.URL + "/"
	env["ACTIONS_RESULTS_URL"] = rs.URL + "/"
	env["ACTIONS_ID_TOKEN_REQUEST_URL"] = rs.URL + "/_services/token"
	env["ACTIONS_ID_TOKEN_REQUEST_TOKEN"] = rs.RuntimeToken
	// Required for @actions/cache to use the twirp v2 api instead of the legacy REST API.
	env["ACTIONS_CACHE_SERVICE_V2"] = "true"
}

// StartServer starts a local GitHub Actions mock server on a random port.
func StartServer(cfg Config) (*RunningServer, error) {
	if cfg.StorageDir == "" {
		cfg.StorageDir = os.TempDir()
	}
	if cfg.OIDCIssuer == "" {
		cfg.OIDCIssuer = "https://token.actions.githubusercontent.com"
	}
	if cfg.OIDCSub == "" {
		cfg.OIDCSub = "repo:local/repo:ref:refs/heads/main"
	}

	cfg.StorageDir = filepath.Clean(cfg.StorageDir)

	// Generate random HMAC signing key
	signingKey := make([]byte, 32)
	if _, err := rand.Read(signingKey); err != nil {
		return nil, fmt.Errorf("generating signing key: %w", err)
	}

	// Listen on random port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listening: %w", err)
	}

	externalURL := fmt.Sprintf("http://%s", listener.Addr().String())

	srv := NewServer(cfg.StorageDir, signingKey, externalURL, OIDCConfig{
		Issuer:  cfg.OIDCIssuer,
		Subject: cfg.OIDCSub,
	})

	httpServer := &http.Server{
		Handler: srv,
	}

	// Generate a minimal runtime token (JWT with Actions.Results scope)
	runtimeToken, err := makeRuntimeToken(signingKey)
	if err != nil {
		listener.Close()
		return nil, fmt.Errorf("generating runtime token: %w", err)
	}

	go func() {
		if err := httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		}
	}()

	return &RunningServer{
		URL:          externalURL,
		RuntimeToken: runtimeToken,
		httpServer:   httpServer,
	}, nil
}

// makeRuntimeToken creates a minimal JWT with an Actions.Results scope.
func makeRuntimeToken(signingKey []byte) (string, error) {
	return mintJWT(signingKey, map[string]any{
		"scp": "Actions.Results:local-run:local-job",
	})
}
