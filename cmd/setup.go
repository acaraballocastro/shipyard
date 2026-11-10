package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"shipyard/internal/bootstrap"
	"shipyard/internal/config"
	"shipyard/internal/prompt"
	"shipyard/internal/remote"
	"shipyard/internal/ui"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
)

// exitRelogin tells a remote caller to reconnect and run setup again.
const exitRelogin = 3

const tokenURL = "https://github.com/settings/tokens/new?scopes=write:packages,admin:public_key&description=shipyard"

var setupGitHubUser string

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Prepare this machine (or --server) to run shipyard",
	Long: `Checks everything shipyard needs and fixes what is missing:
git, Docker (daemon access, Compose v2, Buildx), the /srv/apps and
/srv/repos directories, the ghcr.io login and SSH access to GitHub.
Safe to run again at any time.`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {

		// Only an explicit --server sends setup elsewhere: a default
		// server must not stop you from setting up this machine.
		if serverFlag != "" {
			if t := resolveTarget(); t.remote() {
				os.Exit(remoteSetup(t))
			}
		}

		ui.Section("setup")

		cfg, err := config.LoadOrEmpty()
		if err != nil {
			ui.Fatal("config: %s", err)
		}
		interactive := prompt.Interactive()

		// =========================
		// GITHUB USER
		// =========================

		if setupGitHubUser != "" {
			cfg.GitHubUser = setupGitHubUser
		}
		if interactive && cfg.GitHubUser == "" {
			err := prompt.Run(huh.NewForm(huh.NewGroup(
				huh.NewInput().Title("GitHub user").
					Description("Images are pushed to ghcr.io/<user>/<service>").
					Value(&cfg.GitHubUser).Validate(required("GitHub user")),
			)))
			abortOn(err)
		}
		if cfg.GitHubUser == "" {
			ui.Fatal("GitHub user is required (--github-user)")
		}
		cfg.GitHubUser = strings.TrimSpace(cfg.GitHubUser)
		if err := config.Save(cfg); err != nil {
			ui.Fatal("saving config: %s", err)
		}
		ui.Detail("github", cfg.GitHubUser)
		fmt.Println()

		steps := bootstrap.Steps(func() string { return cfg.GitHubUser }, bootstrap.Inputs{
			Token:        askToken,
			WaitKeyAdded: waitKeyAdded,
		})

		// =========================
		// CHECK
		// =========================

		failing := checkSteps(steps)
		if len(failing) == 0 {
			fmt.Println()
			ui.Ok("this machine is ready for shipyard")
			return
		}

		fmt.Println()
		if !interactive {
			for _, s := range failing {
				ui.Detail(s.Name, s.Hint)
			}
			ui.Fatal("%d requirement(s) missing", len(failing))
		}

		ui.Info("shipyard can fix:")
		for _, s := range failing {
			how := s.Hint
			if s.Fix == nil {
				how = "manual: " + s.Hint
			}
			ui.Detail(s.Name, how)
		}
		fixAll := true
		abortOn(prompt.Run(huh.NewForm(huh.NewGroup(
			huh.NewConfirm().Title("Fix them now? (may ask for your sudo password)").Value(&fixAll),
		))))
		if !fixAll {
			ui.Info("nothing changed")
			os.Exit(1)
		}

		// =========================
		// FIX (in order, rechecking: earlier fixes may solve later steps)
		// =========================

		var still []bootstrap.Step
		for _, s := range failing {
			if detail, err := s.Check(); err == nil {
				ui.CheckOk(s.Name, detail)
				continue
			}
			if s.Fix == nil {
				ui.CheckFail(s.Name, s.Hint)
				still = append(still, s)
				continue
			}
			ui.Step("fix", "%s", s.Name)
			err := s.Fix()
			if errors.Is(err, bootstrap.ErrRelogin) {
				ui.Warn("%s", err)
				ui.Info("then run shipyard setup again to finish")
				os.Exit(exitRelogin)
			}
			if errors.Is(err, prompt.ErrAborted) {
				ui.Info("aborted")
				os.Exit(1)
			}
			if detail, cerr := s.Check(); cerr == nil {
				ui.CheckOk(s.Name, detail)
				continue
			}
			msg := s.Hint
			if err != nil {
				msg = err.Error() + " — " + s.Hint
			}
			ui.CheckFail(s.Name, msg)
			still = append(still, s)
		}

		fmt.Println()
		if len(still) > 0 {
			ui.Fatal("%d requirement(s) still missing, fix them and run shipyard setup again", len(still))
		}
		ui.Ok("this machine is ready for shipyard")
	},
}

// checkSteps prints every check and returns the failing steps.
func checkSteps(steps []bootstrap.Step) []bootstrap.Step {
	var failing []bootstrap.Step
	for _, s := range steps {
		detail, err := s.Check()
		if err != nil {
			ui.CheckFail(s.Name, err.Error())
			failing = append(failing, s)
			continue
		}
		ui.CheckOk(s.Name, detail)
	}
	return failing
}

// remoteSetup runs setup on a server, reconnecting once when the docker
// group change needs a new session. Returns the exit code.
func remoteSetup(t target) int {
	args := []string{remote.Bin, "--server", localTarget, "setup"}
	user := setupGitHubUser
	if user == "" {
		if cfg, err := config.LoadOrEmpty(); err == nil {
			user = cfg.GitHubUser
		}
	}
	if user != "" {
		args = append(args, "--github-user", user)
	}

	ui.Info("running setup on %s (%s)", ui.Colorize("bold", t.Name), t.Server.Host)
	for attempt := 0; attempt < 2; attempt++ {
		err := remote.Run(t.Server.Host, remote.Options{TTY: true}, args...)
		if err == nil {
			return 0
		}
		code := remote.ExitCode(err)
		if code != exitRelogin {
			return code
		}
		ui.Info("reconnecting to %s to apply the docker group", t.Name)
	}
	return 1
}

func askToken() (string, error) {
	var token string
	err := prompt.Run(huh.NewForm(huh.NewGroup(
		huh.NewInput().
			Title("GitHub token").
			Description("Used once for docker login ghcr.io and to add this machine's SSH key.\n" +
				"Scopes: write:packages, admin:public_key — create one at:\n" + tokenURL).
			EchoMode(huh.EchoModePassword).
			Value(&token),
	)))
	return token, err
}

func waitKeyAdded(pub string) error {
	fmt.Println()
	ui.Info("add this SSH key to GitHub: https://github.com/settings/ssh/new")
	fmt.Println()
	fmt.Println("  " + pub)
	fmt.Println()
	for {
		done := true
		if err := prompt.Run(huh.NewForm(huh.NewGroup(
			huh.NewConfirm().Title("Added it? Check again").Affirmative("Check").Negative("Skip").Value(&done),
		))); err != nil {
			return err
		}
		if !done {
			return errors.New("SSH key not added")
		}
		if out, err := bootstrapGitHubCheck(); err == nil {
			ui.Ok("%s", out)
			return nil
		}
		ui.Warn("GitHub does not accept the key yet")
	}
}

// bootstrapGitHubCheck reruns the GitHub SSH check.
func bootstrapGitHubCheck() (string, error) {
	for _, s := range bootstrap.Steps(func() string { return "" }, bootstrap.Inputs{}) {
		if s.Name == "github" {
			return s.Check()
		}
	}
	return "", errors.New("no github step")
}

func abortOn(err error) {
	if errors.Is(err, prompt.ErrAborted) {
		ui.Info("aborted")
		exitQuiet()
	}
	if err != nil {
		ui.Fatal("%s", err)
	}
}

func init() {
	rootCmd.AddCommand(setupCmd)
	setupCmd.Flags().StringVar(&setupGitHubUser, "github-user", "", "GitHub user owning the ghcr.io images")
}
