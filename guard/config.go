package guard

type Config struct {
	Enabled bool

	Network   NetworkConfig
	Redaction RedactionConfig
	Bash      BashConfig

	Audit     AuditConfig
	Approvals ApprovalsConfig
}

type NetworkConfig struct {
	URLFetch URLFetchNetworkPolicy
}

type URLFetchNetworkPolicy struct {
	AllowedURLPrefixes []string
	DenyPrivateIPs     bool
	ResolveDNS         bool // When true, resolve hostnames via DNS and block private IPs.
	FollowRedirects    bool
	AllowProxy         bool
}

type RedactionConfig struct {
	Enabled  bool
	Patterns []RegexPattern
}

type RegexPattern struct {
	Name string
	Re   string
}

type BashConfig struct {
	RequireApproval bool
}

type AuditConfig struct {
	JSONLPath      string
	RotateMaxBytes int64
}

type ApprovalsConfig struct {
	Enabled bool
}
