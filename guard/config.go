package guard

type Config struct {
	Enabled bool

	Network   NetworkConfig
	Redaction RedactionConfig

	Audit     AuditConfig
	Approvals ApprovalsConfig
}

type NetworkConfig struct {
	URLFetch URLFetchNetworkPolicy
}

type URLFetchNetworkPolicy struct {
	// AllowedURLPrefixes are matched literally after basic URL validation.
	AllowedURLPrefixes []string
	DenyPrivateIPs     bool
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

type AuditConfig struct {
	JSONLPath      string
	RotateMaxBytes int64
}

type ApprovalsConfig struct {
	Enabled bool
}
