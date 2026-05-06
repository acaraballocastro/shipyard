package config

type Config struct {
	GitHubUser  string `yaml:"github_user"`
	GitHubToken string `yaml:"github_token"`

	Device struct {
		Host   string `yaml:"host"`
		User   string `yaml:"user"`
		SSHKey string `yaml:"ssh_key"`
	} `yaml:"device"`
}
