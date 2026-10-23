// Package cli is the hl command line.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/DeM-Conve/project__hl/internal/compose"
	"github.com/DeM-Conve/project__hl/internal/config"
	"github.com/DeM-Conve/project__hl/internal/defaults"
	"github.com/DeM-Conve/project__hl/internal/interrupt"
	"github.com/DeM-Conve/project__hl/internal/version"
)

// Exit codes. An interruption exits 128 + the signal (130 for Ctrl-C), and
// `hl shell` exits with the shell's own status.
const (
	exitOK    = 0
	exitFail  = 1 // a command failed: hl's own check, or a docker or git command
	exitUsage = 2 // wrong command, argument or flag
)

// Where commands read and write; tests replace them.
var (
	stdin  io.Reader = os.Stdin
	stdout io.Writer = os.Stdout
	stderr io.Writer = os.Stderr
	goos             = runtime.GOOS
)

var homeFlag string

// Execute runs the command line and exits with its status.
func Execute() { os.Exit(Main(os.Args[1:])) }

// Main runs hl with args and returns the exit code.
func Main(args []string) int {
	ctx, stop := interrupt.Notify(context.Background())
	defer stop()
	return runCLI(ctx, args)
}

func runCLI(ctx context.Context, args []string) int {
	homeFlag = ""
	setColors(stdout)
	c := root()
	c.SetArgs(args)
	c.SetIn(stdin)
	c.SetOut(stdout)
	c.SetErr(stderr)
	err := c.ExecuteContext(ctx)
	if err == nil {
		if ierr := interrupt.Err(ctx); ierr != nil {
			return interrupt.ExitCode(ierr)
		}
		return exitOK
	}
	return report(err)
}

// report prints err (what failed, then what to do) and returns the exit code.
func report(err error) int {
	if errors.Is(err, interrupt.ErrInterrupted) {
		fmt.Fprintf(stderr, "%sinterrupted%s\n", dim, reset)
		return interrupt.ExitCode(err)
	}
	var st *exitStatus
	if errors.As(err, &st) {
		return st.code
	}
	printErr(err)
	var u *usageError
	if errors.As(err, &u) || isCobraUsage(err) {
		return exitUsage
	}
	return exitFail
}

func printErr(err error) {
	fmt.Fprintf(stderr, "%serror:%s %v\n", red, reset, err)
	if h := hintFor(err); h != "" {
		fmt.Fprintf(stderr, "%shint:%s %s\n", yellow, reset, h)
	}
}

// exitStatus ends hl with a given code and no message (the command already said why).
type exitStatus struct{ code int }

func (e *exitStatus) Error() string { return fmt.Sprintf("exit status %d", e.code) }

// usageError is a wrong command, argument or flag.
type usageError struct {
	err error
	cmd string
}

func (e *usageError) Error() string { return e.err.Error() }
func (e *usageError) Unwrap() error { return e.err }

// hintError carries what the user can do about err.
type hintError struct {
	err  error
	hint string
}

func (e *hintError) Error() string { return e.err.Error() }
func (e *hintError) Unwrap() error { return e.err }

func withHint(err error, format string, a ...any) error {
	if err == nil {
		return nil
	}
	return &hintError{err: err, hint: fmt.Sprintf(format, a...)}
}

func isCobraUsage(err error) bool {
	s := err.Error()
	for _, p := range []string{"unknown command", "unknown flag", "unknown shorthand flag", "flag needs an argument", "invalid argument", "bad flag syntax"} {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// hintFor is the advice for err: its own, or one read from docker's error output.
func hintFor(err error) string {
	var h *hintError
	if errors.As(err, &h) {
		return h.hint
	}
	var u *usageError
	if errors.As(err, &u) {
		return fmt.Sprintf("see `%s --help`", u.cmd)
	}
	if isCobraUsage(err) {
		return "see `hl --help`"
	}
	var ce *compose.Error
	if errors.As(err, &ce) {
		if errors.Is(ce.Err, exec.ErrNotFound) {
			return "install Docker (https://docs.docker.com/engine/install/) and make sure `docker` is on your PATH, then run `hl doctor`"
		}
		return dockerHint(ce.Stderr)
	}
	return ""
}

// dockerHint explains a docker error message, or returns "".
func dockerHint(msg string) string {
	m := strings.ToLower(msg)
	switch {
	case strings.Contains(m, "permission denied") && strings.Contains(m, "docker"):
		return "your user may not use Docker - add it to the docker group (`sudo usermod -aG docker $USER`, then log in again)"
	case strings.Contains(m, "cannot connect to the docker daemon"), strings.Contains(m, "is the docker daemon running"),
		strings.Contains(m, "error during connect"), strings.Contains(m, "docker.sock: connect"):
		return "the Docker daemon isn't running - start Docker (Linux: `sudo systemctl start docker`; macOS: open Docker Desktop), then try again"
	case strings.Contains(m, "port is already allocated"), strings.Contains(m, "address already in use"):
		return "another program already uses that port - stop it, or set proxy.port in hl.yml (`hl doctor` names the program)"
	case strings.Contains(m, "!reset"), strings.Contains(m, "unknown tag"):
		return fmt.Sprintf("hl needs docker compose v%d.%d or newer - update the compose plugin, then run `hl doctor`", defaults.ComposeMinMajor, defaults.ComposeMinMinor)
	case strings.Contains(m, "pool overlaps"):
		return "proxy.subnet overlaps another Docker network - pick another subnet (`hl doctor` suggests a free one)"
	case strings.Contains(m, "non-overlapping ipv4 address pool"), strings.Contains(m, "available, non-overlapping"):
		return "Docker ran out of network address ranges - remove unused networks (`docker network prune`) or set proxy.subnet in hl.yml"
	case strings.Contains(m, "network") && (strings.Contains(m, "not found") || strings.Contains(m, "could not be found")):
		return "the proxy's network is missing - `hl proxy up` (or `hl up`) creates it"
	case strings.Contains(m, "is already in use by container"):
		return "a container with that name already exists (from another project or homelab) - remove it with `docker rm -f <name>`, or give this homelab a `name:` in hl.yml"
	case strings.Contains(m, "pull access denied"), strings.Contains(m, "manifest unknown"), strings.Contains(m, "not found: manifest"):
		return "an image couldn't be pulled - check the image name and tag, and `docker login` if it is private"
	case strings.Contains(m, "no space left on device"):
		return "the disk is full - free space (`docker system prune` removes unused images)"
	}
	return ""
}

func root() *cobra.Command {
	c := &cobra.Command{
		Use:   "hl",
		Short: "Run your self-hosted apps behind one proxy, each from its own folder, unmodified",
		Long: `hl runs apps that each come with their own docker compose file behind one
Traefik proxy, from a homelab folder that holds the config (hl.yml) and every
app's data (data/<app>/).

  hl init ~/homelab           create the homelab folder
  hl add ~/code/my-app        add an app (its folder, with a compose file)
  hl up                       start everything; hl status shows the URLs`,
		Version:       version.Get().Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// Commands write where cobra was told to (run, or a test).
			stdin, stdout, stderr = cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr()
			return supported(cmd)
		},
	}
	c.SetVersionTemplate(version.Get().Full() + "\n")
	c.PersistentFlags().StringVar(&homeFlag, "home", "", "homelab folder (default: $"+defaults.HomeEnv+", the nearest folder with an hl.yml, or the one from `hl init`)")
	c.AddCommand(initCmd(), addCmd(), removeCmd(), statusCmd(),
		upCmd(), downCmd(), restartCmd(), rebuildCmd(), updateCmd(),
		logsCmd(), psCmd(), openCmd(), shellCmd(), configCmd(), pathCmd(),
		proxyCmd(), doctorCmd(), versionCmd(), syncCmd(),
		setCmd(), unsetCmd(), editCmd())
	c.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return &usageError{err: err, cmd: cmd.CommandPath()}
	})
	wrapArgs(c)
	return c
}

// wrapArgs makes every command's argument errors usage errors (exit 2).
func wrapArgs(c *cobra.Command) {
	for _, sub := range c.Commands() {
		wrapArgs(sub)
	}
	if c.Args == nil {
		return
	}
	check := c.Args
	c.Args = func(cmd *cobra.Command, args []string) error {
		if err := check(cmd, args); err != nil {
			return &usageError{err: err, cmd: cmd.CommandPath()}
		}
		return nil
	}
}

// supported refuses to run on Windows, except for what needs no Docker.
func supported(cmd *cobra.Command) error {
	if goos != "windows" {
		return nil
	}
	switch cmd.Name() {
	case "version", "help", "completion":
		return nil
	}
	return withHint(errors.New("hl doesn't support Windows: it drives Docker's Linux containers with Unix paths and sockets"),
		"run hl inside WSL 2 (e.g. Ubuntu) with Docker available there, or on Linux or macOS")
}

func load() (*config.Config, error) {
	home, err := config.Home(homeFlag)
	if err != nil {
		return nil, err
	}
	return config.Load(home)
}

// targets are the apps named on the command line, or all of them.
func targets(cfg *config.Config, args []string) ([]string, error) {
	if len(args) == 0 {
		if len(cfg.Apps) == 0 {
			return nil, withHint(errors.New("no apps in "+cfg.Dir), "add one with `hl add <folder>`")
		}
		return cfg.Names(), nil
	}
	seen := map[string]bool{}
	var names []string
	for _, a := range args {
		if _, err := cfg.App(a); err != nil {
			return nil, withHint(err, "`hl status` lists the apps")
		}
		if !seen[a] {
			seen[a] = true
			names = append(names, a)
		}
	}
	return names, nil
}

// completeApps completes app names from hl.yml, leaving out the ones already typed.
func completeApps(cmd *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	cfg, err := load()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return without(cfg.Names(), args), cobra.ShellCompDirectiveNoFileComp
}

// completeApp completes one app name: the command's only argument.
func completeApp(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return completeApps(cmd, args, toComplete)
}

// completeAppServices completes an app name, then that app's services.
func completeAppServices(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return completeApps(cmd, args, toComplete)
	}
	cfg, err := load()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	a, err := cfg.App(args[0])
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	p, err := project(cfg, args[0], a)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	m, err := p.Model(cmd.Context())
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	services := make([]string, 0, len(m.Services))
	for s := range m.Services {
		services = append(services, s)
	}
	sort.Strings(services)
	return without(services, args[1:]), cobra.ShellCompDirectiveNoFileComp
}

// without is names minus the ones in typed.
func without(names, typed []string) []string {
	seen := map[string]bool{}
	for _, t := range typed {
		seen[t] = true
	}
	out := make([]string, 0, len(names))
	for _, n := range names {
		if !seen[n] {
			out = append(out, n)
		}
	}
	return out
}
