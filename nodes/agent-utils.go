package nodes

import (
	"os"

	"github.com/actionforge/actrun-cli/core"
)

// envOrOs looks up a key in the graph's env map first, then falls back to the
// OS environment. The graph env (c.Env) only contains values from config files,
// .env files, and overrides — it does not include OS env vars set by the agent
// worker (BUILD_SERVER_URL, BUILD_AGENT_TOKEN, etc.).
func envOrOs(c *core.ExecutionState, key string) string {
	if v := c.Env[key]; v != "" {
		return v
	}
	return os.Getenv(key)
}
