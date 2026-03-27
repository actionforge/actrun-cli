package agent

import (
	"context"
	"fmt"

	"github.com/actionforge/actrun-cli/agent/vault"
	"github.com/sirupsen/logrus"
)

// FetchVaultSecrets connects to each BYOV Vault config, reads secrets, and returns
// a merged env map plus a list of secret values for log masking.
func FetchVaultSecrets(ctx context.Context, configs []VaultFetchConfig) (env map[string]string, secretValues []string, err error) {
	env = make(map[string]string)

	for _, cfg := range configs {
		vcfg := vault.Config{
			Address:   cfg.Addr,
			Token:     cfg.Token,
			RoleID:    cfg.RoleID,
			SecretID:  cfg.SecretID,
			MountPath: cfg.MountPath,
			Namespace: cfg.Namespace,
		}

		client, err := vault.NewClient(vcfg)
		if err != nil {
			logrus.WithError(err).WithField("vault_addr", cfg.Addr).Warn("failed to connect to vault")
			return nil, nil, fmt.Errorf("vault connect %s: %w", cfg.Addr, err)
		}

		secrets, err := client.ReadSecrets(ctx, cfg.Path)
		if err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{
				"vault_addr": cfg.Addr,
				"path":       cfg.Path,
			}).Warn("failed to read vault secrets")
			return nil, nil, fmt.Errorf("vault read %s: %w", cfg.Path, err)
		}

		for k, v := range secrets {
			env[k] = v
			secretValues = append(secretValues, v)
		}
	}

	return env, secretValues, nil
}
