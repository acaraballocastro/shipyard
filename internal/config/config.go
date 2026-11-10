package config

type Config struct {
	GitHubUser  string `yaml:"github_user"`
	GitHubToken string `yaml:"github_token"`

	Device struct {
		Host   string `yaml:"host"`
		User   string `yaml:"user"`
		SSHKey string `yaml:"ssh_key"`
	} `yaml:"device"`

	// Servers are remote hosts reachable over SSH that run shipyard.
	Servers map[string]Server `yaml:"servers,omitempty"`
	// DefaultServer is used when --server is not given. Empty means local.
	DefaultServer string `yaml:"default_server,omitempty"`
}

// Server is a remote shipyard host.
type Server struct {
	// Host is passed to ssh: user@host, or an alias from ~/.ssh/config.
	Host string `yaml:"host"`
}
