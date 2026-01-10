package sessions

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const (
	// this is the gateway url, the same that the web app uses to obtain a session id
	apiGatewayUrl = "app.actionforge.dev"

	APP_URL = "app.actionforge.dev"
)

type StartSessionResponse struct {
	DebugSessionId string `json:"debug_session_id"`
}

type SessionData struct {
	SessionID string
	RawKey    []byte
	Token     string
}

func GetGatewayURL() string {
	gatewayHost := os.Getenv("ACT_SESSION_GATEWAY")
	if gatewayHost == "" {
		gatewayHost = apiGatewayUrl
	}
	return gatewayHost
}

// Posts to the gateway to get a session token and id.
func StartNewSession(httpScheme, gatewayUrl string) (*SessionData, error) {
	apiURL := fmt.Sprintf("%s://%s/api/v2/session/start?debug=true", httpScheme, gatewayUrl)

	client := &http.Client{Timeout: 10 * time.Second}

	req, err := http.NewRequest("POST", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to contact server: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("server error %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var result StartSessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode JSON response: %w", err)
	}

	if result.DebugSessionId == "" {
		return nil, fmt.Errorf("received empty debug_session_id from server")
	}

	rawKey := make([]byte, 32) // 256 bits
	if _, err := rand.Read(rawKey); err != nil {
		return nil, fmt.Errorf("failed to generate random key: %w", err)
	}

	token, err := createFullSessionToken(result.DebugSessionId, rawKey)
	if err != nil {
		return nil, fmt.Errorf("failed to generate token: %w", err)
	}

	return &SessionData{
		SessionID: result.DebugSessionId,
		RawKey:    rawKey,
		Token:     token,
	}, nil
}

func createFullSessionToken(sessionId string, rawKey []byte) (string, error) {
	sessionBytes := []byte(sessionId)

	if len(sessionBytes) > 255 {
		return "", fmt.Errorf("session ID length exceeds maximum of 255 bytes")
	}

	// construct data to hash: session bytes + raw key
	dataToHash := make([]byte, 0, len(sessionBytes)+len(rawKey))
	dataToHash = append(dataToHash, sessionBytes...)
	dataToHash = append(dataToHash, rawKey...)

	hash := sha256.Sum256(dataToHash)
	checksum := hash[:4] // first 4 bytes

	// construct the final packet here
	// [len session id (1 byte)] + [session id] + [raw kye] + [checksum]
	packet := make([]byte, 0, 1+len(sessionBytes)+len(rawKey)+len(checksum))
	packet = append(packet, byte(len(sessionBytes)))
	packet = append(packet, sessionBytes...)
	packet = append(packet, rawKey...)
	packet = append(packet, checksum...)

	return base64.StdEncoding.EncodeToString(packet), nil
}
