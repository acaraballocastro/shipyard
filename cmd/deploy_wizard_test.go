package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/pflag"
)

func wizardFlags() *pflag.FlagSet {
	fs := pflag.NewFlagSet("t", pflag.ContinueOnError)
	var s string
	var b bool
	var l []string
	for _, n := range []string{"repo", "image", "name", "mode", "domain", "network", "internal-port", "cert-resolver", "env-file", "healthcheck-cmd", "platform"} {
		fs.StringVar(&s, n, "", "")
	}
	fs.BoolVar(&b, "tls", false, "")
	fs.BoolVar(&b, "create-network", false, "")
	for _, n := range []string{"port", "volume", "env", "build-arg"} {
		fs.StringArrayVar(&l, n, nil, "")
	}
	return fs
}

func TestWizardApplyTraefik(t *testing.T) {
	a := wizardAnswers{source: "repo", repo: "user/app", mode: "traefik", domain: "a.example.com",
		network: "edge", internalPort: "3000", tls: true, createNet: true,
		extras: []string{extraEnv}, env: "A=1\nSECRET=hunter2\n"}
	fs := wizardFlags()
	if err := a.apply(fs); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"repo", "mode", "domain", "network", "internal-port", "tls", "create-network", "env"} {
		if !fs.Changed(f) {
			t.Errorf("flag %s not marked changed", f)
		}
	}
	if fs.Changed("port") || fs.Changed("image") {
		t.Error("unexpected flags set")
	}
	cmd := a.command()
	if strings.Contains(cmd, "hunter2") {
		t.Errorf("secret leaked in command: %s", cmd)
	}
	for _, want := range []string{"--repo user/app", "--create-network", "--tls", "--internal-port 3000", "--env SECRET=***"} {
		if !strings.Contains(cmd, want) {
			t.Errorf("command missing %q:\n%s", want, cmd)
		}
	}
}

func TestWizardApplyLocalPorts(t *testing.T) {
	a := wizardAnswers{source: "image", image: "traefik/whoami", mode: "local", ports: "8080:80, 9090:90"}
	fs := wizardFlags()
	if err := a.apply(fs); err != nil {
		t.Fatal(err)
	}
	got, _ := fs.GetStringArray("port")
	if len(got) != 2 || got[1] != "9090:90" {
		t.Errorf("ports = %v", got)
	}
	if fs.Changed("domain") {
		t.Error("domain set in local mode")
	}
}
