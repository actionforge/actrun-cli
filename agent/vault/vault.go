package vault

import (
	"context"
	"fmt"
	"path"

	vaultapi "github.com/hashicorp/vault/api"
)

// Config holds the configuration for connecting to a Vault instance.
type Config struct {
	Address   string
	Token     string
	RoleID    string
	SecretID  string
	MountPath string
	Namespace string
}

// Client wraps a Vault API client for KV v2 operations.
type Client struct {
	client    *vaultapi.Client
	mountPath string
}

// NewClient creates a new Vault client. It authenticates via token or AppRole.
func NewClient(cfg Config) (*Client, error) {
	vcfg := vaultapi.DefaultConfig()
	vcfg.Address = cfg.Address

	client, err := vaultapi.NewClient(vcfg)
	if err != nil {
		return nil, fmt.Errorf("vault client: %w", err)
	}

	if cfg.Namespace != "" {
		client.SetNamespace(cfg.Namespace)
	}

	mountPath := cfg.MountPath
	if mountPath == "" {
		mountPath = "secret"
	}

	if cfg.Token != "" {
		client.SetToken(cfg.Token)
	} else if cfg.RoleID != "" && cfg.SecretID != "" {
		secret, err := client.Logical().Write("auth/approle/login", map[string]interface{}{
			"role_id":   cfg.RoleID,
			"secret_id": cfg.SecretID,
		})
		if err != nil {
			return nil, fmt.Errorf("vault approle login: %w", err)
		}
		if secret == nil || secret.Auth == nil {
			return nil, fmt.Errorf("vault approle login: empty response")
		}
		client.SetToken(secret.Auth.ClientToken)
	} else {
		return nil, fmt.Errorf("vault: either token or role_id+secret_id required")
	}

	return &Client{client: client, mountPath: mountPath}, nil
}

// SecretsResult holds key-value pairs plus KV v2 version metadata.
type SecretsResult struct {
	Data        map[string]string
	CreatedTime string
	UpdatedTime string
}

// ReadSecrets reads all key-value pairs from a KV v2 path.
// Returns nil (not error) if the path does not exist.
func (c *Client) ReadSecrets(ctx context.Context, secretPath string) (map[string]string, error) {
	res, err := c.ReadSecretsWithMetadata(ctx, secretPath)
	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, nil
	}
	return res.Data, nil
}

// ReadSecretsWithMetadata reads all key-value pairs plus version metadata.
func (c *Client) ReadSecretsWithMetadata(ctx context.Context, secretPath string) (*SecretsResult, error) {
	fullPath := path.Join(c.mountPath, "data", secretPath)
	secret, err := c.client.Logical().ReadWithContext(ctx, fullPath)
	if err != nil {
		return nil, fmt.Errorf("vault read %s: %w", secretPath, err)
	}
	if secret == nil || secret.Data == nil {
		return nil, nil
	}

	dataRaw, ok := secret.Data["data"]
	if !ok {
		return nil, nil
	}

	dataMap, ok := dataRaw.(map[string]interface{})
	if !ok {
		return nil, nil
	}

	result := &SecretsResult{
		Data: make(map[string]string, len(dataMap)),
	}
	for k, v := range dataMap {
		if s, ok := v.(string); ok {
			result.Data[k] = s
		}
	}

	// Extract metadata timestamps
	if meta, ok := secret.Data["metadata"].(map[string]interface{}); ok {
		if ct, ok := meta["created_time"].(string); ok {
			result.CreatedTime = ct
		}
	}

	return result, nil
}
