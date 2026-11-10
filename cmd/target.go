package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"shipyard/internal/config"
	"shipyard/internal/remote"
	"shipyard/internal/state"
	"shipyard/internal/ui"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// localTarget forces the local machine, overriding default_server.
const localTarget = "local"

// serverFlag is the global --server value.
var serverFlag string

// target is a resolved --server: nil Server means this machine.
type target struct {
	Name   string
	Server *config.Server
}

func (t target) remote() bool { return t.Server != nil }

// resolveTarget picks the server from --server, then default_server.
func resolveTarget() target {
	name := serverFlag
	cfg, err := config.LoadOrEmpty()
	if err != nil {
		ui.Fatal("config: %s", err)
	}
	if name == "" {
		name = cfg.DefaultServer
	}
	if name == "" || name == localTarget {
		return target{Name: localTarget}
	}
	srv, ok := cfg.Servers[name]
	if !ok {
		ui.Fatal("unknown server %q (see: shipyard server list)", name)
	}
	return target{Name: name, Server: &srv}
}

// forwardedFlags renders the flags the user set as --name=value arguments,
// skipping the ones in skip. Repeatable flags are expanded.
func forwardedFlags(fs *pflag.FlagSet, skip ...string) []string {
	skipped := map[string]bool{"server": true, "no-input": true}
	for _, s := range skip {
		skipped[s] = true
	}
	var out []string
	fs.Visit(func(f *pflag.Flag) {
		if skipped[f.Name] {
			return
		}
		if sv, ok := f.Value.(pflag.SliceValue); ok {
			for _, v := range sv.GetSlice() {
				out = append(out, "--"+f.Name+"="+v)
			}
			return
		}
		out = append(out, "--"+f.Name+"="+f.Value.String())
	})
	return out
}

// remoteArgs is the shipyard argv to run on the server for cmd.
func remoteArgs(cmd *cobra.Command, positional []string, flags []string) []string {
	args := []string{remote.Bin, "--server", localTarget, cmd.Name()}
	args = append(args, flags...)
	if len(positional) > 0 {
		args = append(args, "--")
		args = append(args, positional...)
	}
	return args
}

// forward runs cmd on the remote target with a terminal and exits with the
// remote status. It never returns.
func forward(t target, cmd *cobra.Command, positional []string) {
	ui.Info("running on %s (%s)", ui.Colorize("bold", t.Name), t.Server.Host)
	err := remote.Run(t.Server.Host, remote.Options{TTY: true},
		remoteArgs(cmd, positional, forwardedFlags(cmd.Flags()))...)
	if err != nil {
		os.Exit(remote.ExitCode(err))
	}
	os.Exit(0)
}

// loadApps returns the deployed services of a target.
func loadApps(t target) (map[string]state.App, error) {
	if !t.remote() {
		st, err := state.Load()
		if err != nil {
			return nil, fmt.Errorf("state: %w", err)
		}
		return st.Apps, nil
	}
	out, err := remote.Output(t.Server.Host, remote.Bin, "--server", localTarget, "list", "--json")
	if err != nil {
		return nil, fmt.Errorf("listing services on %s: %w (is shipyard up to date there? try: shipyard server add %s --host %s)",
			t.Name, err, t.Name, t.Server.Host)
	}
	apps := map[string]state.App{}
	if err := json.Unmarshal(out, &apps); err != nil {
		return nil, fmt.Errorf("parsing services from %s: %w", t.Name, err)
	}
	return apps, nil
}

func sortedServerNames(m map[string]config.Server) []string {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
