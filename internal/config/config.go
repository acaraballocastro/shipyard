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
	// URL and Token reach `shipyard serve` on the server (web UI/API).
	URL   string `yaml:"url,omitempty"`
	Token string `yaml:"token,omitempty"`
	// Routes are alternative ways to reach the API, most preferred first
	// (e.g. Cloudflare, then a VPN). Empty means: URL, then an SSH tunnel.
	Routes []Route `yaml:"routes,omitempty"`
	// NoSSHFallback disables the automatic SSH tunnel route.
	NoSSHFallback bool `yaml:"no_ssh_fallback,omitempty"`
}

// Route is one way to reach a server's API.
type Route struct {
	Name string `yaml:"name"`
	URL  string `yaml:"url"`
	// Headers are sent with every request on this route, e.g. a
	// Cloudflare Access service token (CF-Access-Client-Id/-Secret).
	Headers map[string]string `yaml:"headers,omitempty"`
}

// SSHRoute is the name of the automatic SSH tunnel route.
const SSHRoute = "ssh"

// EffectiveRoutes returns the routes to try in order: the configured
// ones (or URL as "lan"), then the SSH tunnel unless disabled. An SSH
// route has an empty URL.
func (s Server) EffectiveRoutes() []Route {
	var out []Route
	if len(s.Routes) > 0 {
		out = append(out, s.Routes...)
	} else if s.URL != "" {
		out = append(out, Route{Name: "lan", URL: s.URL})
	}
	if s.Host != "" && !s.NoSSHFallback && s.URL != "" {
		out = append(out, Route{Name: SSHRoute})
	}
	return out
}
