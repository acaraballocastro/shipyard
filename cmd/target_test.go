package cmd

import (
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/pflag"
)

func TestForwardedFlags(t *testing.T) {
	fs := pflag.NewFlagSet("t", pflag.ContinueOnError)
	var s, srv string
	var b bool
	var l, e []string
	fs.StringVar(&s, "repo", "", "")
	fs.StringVar(&srv, "server", "", "")
	fs.BoolVar(&b, "tls", false, "")
	fs.StringArrayVar(&l, "port", nil, "")
	fs.StringArrayVar(&e, "env", nil, "")
	fs.StringVar(&s, "domain", "default.example", "")
	if err := fs.Parse([]string{"--repo", "u/a", "--server", "pi", "--tls", "--port", "1:1", "--port", "2:2", "--env", "K=secret"}); err != nil {
		t.Fatal(err)
	}
	got := forwardedFlags(fs, "env")
	want := []string{"--port=1:1", "--port=2:2", "--repo=u/a", "--tls=true"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestReadEnvStdin(t *testing.T) {
	envVars = nil
	defer func() { envVars = nil }()
	if err := readEnvStdin(strings.NewReader("A=1\r\n\nB=x y\n")); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(envVars, []string{"A=1", "B=x y"}) {
		t.Errorf("envVars = %q", envVars)
	}
}
