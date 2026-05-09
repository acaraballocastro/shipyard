package state

import (
	"os"

	"gopkg.in/yaml.v3"
)

type App struct {
	Repo       string `yaml:"repo"`
	Image      string `yaml:"image"`
	Mode       string `yaml:"mode"`
	Domain     string `yaml:"domain,omitempty"`
	Network    string `yaml:"network,omitempty"`
	Path       string `yaml:"path"`
	LastCommit string `yaml:"last_commit,omitempty"`
}

type State struct {
	Apps map[string]App `yaml:"apps"`
}

func GetPath() string {
	home, _ := os.UserHomeDir()
	return home + "/.shipyard-state.yaml"
}

func Load() (*State, error) {
	path := GetPath()

	data, err := os.ReadFile(path)
	if err != nil {
		return &State{Apps: map[string]App{}}, nil
	}

	var s State
	err = yaml.Unmarshal(data, &s)
	return &s, err
}

func Save(s *State) error {
	data, err := yaml.Marshal(s)
	if err != nil {
		return err
	}

	return os.WriteFile(GetPath(), data, 0600)
}
