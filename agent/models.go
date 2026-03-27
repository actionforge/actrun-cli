package agent

// RunStatus represents the state of a build run.
type RunStatus string

const (
	RunPending   RunStatus = "pending"
	RunClaimed   RunStatus = "claimed"
	RunRunning   RunStatus = "running"
	RunSuccess   RunStatus = "success"
	RunFailure   RunStatus = "failure"
	RunCancelled RunStatus = "cancelled"
)

// ClaimResponse is returned when an agent claims a run from the queue.
type ClaimResponse struct {
	RunID         string              `json:"run_id"`
	JobID         string              `json:"job_id,omitempty"`
	Owner         string              `json:"owner"`
	Name          string              `json:"name"`
	Pipeline      string              `json:"pipeline"`
	Ref           string              `json:"ref"`
	VCSType       string              `json:"vcs_type"`
	VCSURL        string              `json:"vcs_url"`
	Env           map[string]string   `json:"env"`
	VaultConfigs  []VaultFetchConfig  `json:"vault_configs,omitempty"`
	StorageConfig *StorageFetchConfig `json:"storage_config,omitempty"`
	MatrixValues  map[string]string      `json:"matrix_values,omitempty"`
	EnvMappings   map[string]interface{} `json:"env_mappings,omitempty"`
}

// StorageFetchConfig provides external storage details for orchestrator-managed modes.
// In agent-direct mode, the orchestrator does not send storage config — the agent
// operator configures BUILD_STORAGE_* env vars on the host directly.
type StorageFetchConfig struct {
	Provider string `json:"storage_provider"`
	Bucket   string `json:"storage_bucket"`
	Region   string `json:"storage_region"`
	Endpoint string `json:"storage_endpoint,omitempty"`
	Prefix   string `json:"storage_prefix"`
	Mode     string `json:"storage_mode"`
}

// VaultFetchConfig is sent to the agent so it can connect to the user's Vault directly (BYOV).
type VaultFetchConfig struct {
	Addr       string `json:"vault_addr"`
	AuthMethod string `json:"auth_method"`
	Token      string `json:"vault_token,omitempty"`
	RoleID     string `json:"role_id,omitempty"`
	SecretID   string `json:"secret_id,omitempty"`
	MountPath  string `json:"mount_path"`
	Path       string `json:"path"`
	Namespace  string `json:"namespace,omitempty"`
}

// LogBatch is a batch of log entries sent to the server.
type LogBatch struct {
	Lines []LogEntry `json:"lines"`
}

// LogEntry is a single log line.
type LogEntry struct {
	LineNum int    `json:"line_num"`
	Stream  string `json:"stream"`
	Content string `json:"content"`
}

// StatusReport is sent to update the status of a run.
type StatusReport struct {
	Status   RunStatus `json:"status"`
	ExitCode *int      `json:"exit_code,omitempty"`
}

// HeartbeatRequest is sent periodically with agent metrics.
type HeartbeatRequest struct {
	UUID          string  `json:"uuid,omitempty"`
	CPUPercent    float64 `json:"cpu_percent"`
	MemPercent    float64 `json:"mem_percent"`
	MemUsedBytes  int64   `json:"mem_used_bytes"`
	MemTotalBytes int64   `json:"mem_total_bytes"`
	NetRxBytes    int64   `json:"net_rx_bytes"`
	NetTxBytes    int64   `json:"net_tx_bytes"`
}

// ActiveNode represents a currently executing node in a graph pipeline.
type ActiveNode struct {
	NodeID   string `json:"node_id"`
	NodeName string `json:"node_name"`
}
