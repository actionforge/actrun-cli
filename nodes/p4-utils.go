//go:build p4

package nodes

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/actionforge/actrun-cli/core"
	p4go "github.com/perforce/p4go"
)

// P4Credentials bundles Perforce connection details into a credential object.
type P4Credentials struct {
	Port     string
	User     string
	Password string
	Trust    string
	Client   string
}

func (p P4Credentials) Type() core.CredentialType {
	return core.CredentialTypeP4
}

// p4EnvFields holds resolved P4 connection details from credentials and/or environment.
type p4EnvFields struct {
	Port     string
	User     string
	Password string
	Trust    string
	Client   string
}

// buildP4Fields resolves P4 connection details from credentials (if provided)
// with fallback to the graph/OS environment for any empty field.
func buildP4Fields(c *core.ExecutionState, creds core.Credentials) p4EnvFields {
	var p4 P4Credentials
	if creds != nil {
		if pc, ok := creds.(P4Credentials); ok {
			p4 = pc
		}
	}

	resolve := func(credVal, envKey string) string {
		if credVal != "" {
			return credVal
		}
		return envOrOs(c, envKey)
	}

	return p4EnvFields{
		Port:     resolve(p4.Port, "P4PORT"),
		User:     resolve(p4.User, "P4USER"),
		Password: resolve(p4.Password, "P4PASSWD"),
		Trust:    resolve(p4.Trust, "P4TRUST"),
		Client:   resolve(p4.Client, "P4CLIENT"),
	}
}

// connectP4 creates a p4go client, establishes trust for SSL, authenticates,
// and returns the connected client. Caller must call p4.Disconnect() and p4.Close().
func connectP4(c *core.ExecutionState, fields p4EnvFields) (*p4go.P4, error) {
	p4 := p4go.New()

	if fields.Port == "" {
		return nil, core.CreateErr(c, nil, "P4PORT is not set. Provide it via credentials or environment variable.")
	}
	p4.SetPort(fields.Port)

	if fields.User != "" {
		p4.SetUser(fields.User)
	}
	if fields.Password != "" {
		p4.SetPassword(fields.Password)
	}
	if fields.Client != "" {
		p4.SetClient(fields.Client)
	}

	// SSL trust handling
	if strings.HasPrefix(fields.Port, "ssl:") {
		trustFile := fields.Trust
		if trustFile == "" {
			trustFile = filepath.Join(os.TempDir(), ".p4trust")
		}
		p4.SetTrustFile(trustFile)
	}

	connected, err := p4.Connect()
	if !connected {
		p4.Close()
		return nil, core.CreateErr(c, err, "failed to connect to Perforce server at %s", fields.Port)
	}

	// Accept SSL fingerprint
	if strings.HasPrefix(fields.Port, "ssl:") {
		p4.Run("trust", "-y")
	}

	// Authenticate if password is set
	if fields.Password != "" {
		if _, err := p4.RunLogin(); err != nil {
			p4.Disconnect()
			p4.Close()
			return nil, core.CreateErr(c, err, "P4 login failed for user %s", fields.User)
		}
	}

	return p4, nil
}

// runP4Cmd connects, runs a p4 command, and returns the output.
func runP4Cmd(c *core.ExecutionState, fields p4EnvFields, cmd string, args ...string) (string, error) {
	p4, err := connectP4(c, fields)
	if err != nil {
		return "", err
	}
	defer func() {
		p4.Disconnect()
		p4.Close()
	}()

	results, err := p4.Run(cmd, args...)
	if err != nil {
		return "", core.CreateErr(c, err, "p4 %s failed", cmd)
	}

	return formatP4Results(results), nil
}

// formatP4Results converts p4go results to a human-readable string.
func formatP4Results(results []p4go.P4Result) string {
	var lines []string
	for _, r := range results {
		switch v := r.(type) {
		case p4go.P4Data:
			if s := strings.TrimSpace(string(v)); s != "" {
				lines = append(lines, s)
			}
		case p4go.Dictionary:
			line := formatP4Dict(v)
			if line != "" {
				lines = append(lines, line)
			}
		case p4go.P4Message:
			if s := v.String(); s != "" {
				lines = append(lines, strings.TrimSpace(s))
			}
		default:
			lines = append(lines, fmt.Sprintf("%v", r))
		}
	}
	return strings.Join(lines, "\n")
}

// formatP4Dict formats a p4go Dictionary into a human-readable line.
func formatP4Dict(d p4go.Dictionary) string {
	if depotFile, ok := d["depotFile"]; ok {
		rev, _ := d["rev"]
		action, _ := d["action"]
		change, _ := d["change"]
		fileType, _ := d["type"]
		return fmt.Sprintf("%v#%v - %v change %v (%v)", depotFile, rev, action, change, fileType)
	}
	// Generic fallback
	var parts []string
	for k, v := range d {
		parts = append(parts, fmt.Sprintf("%s=%v", k, v))
	}
	return strings.Join(parts, " ")
}
