// Command sbxgo manages project-level AI coding agent sandboxes built on Docker Sandboxes.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/rotisserie/eris"

	"github.com/HenrikPoulsen/sbxgo/internal/fsutil"
	"github.com/HenrikPoulsen/sbxgo/internal/prompt"
	"github.com/HenrikPoulsen/sbxgo/internal/runner"
	"github.com/HenrikPoulsen/sbxgo/internal/sandbox"
	"github.com/spf13/cobra"
)

// Populated by goreleaser at build time via -ldflags -X.
// "dev" / "" defaults are what you see from a plain `go build`.
//
//nolint:gochecknoglobals // ldflag injection target, must be a package-level var
var (
	version = "dev"
	commit  = ""
	date    = ""
)

func main() {
	os.Exit(run())
}

func versionString() string {
	if commit == "" && date == "" {
		return version
	}

	return fmt.Sprintf("%s (commit %s, built %s)", version, commit, date)
}

func run() int {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	rootCmd, debug := newRootCmd()
	if err := rootCmd.ExecuteContext(ctx); err != nil {
		// Default: human-readable colon-chained message.
		// --debug: include eris stack frames so bug reports are actionable.
		fmt.Fprintln(os.Stderr, "sbxgo: "+eris.ToString(err, *debug))

		return 1
	}

	return 0
}

func newRootCmd() (*cobra.Command, *bool) {
	var debug bool

	cmd := &cobra.Command{
		Use:   "sbxgo",
		Short: "Run AI coding agents in Docker Sandboxes",
		Long: `sbxgo manages project-level AI coding agent sandboxes built on Docker Sandboxes.

Each project owns its own .sbxgo/config.toml committed to the repo, so the whole team
gets the same sandbox without per-machine setup. The config can point at a Dockerfile
to build, or a published image to pull.

Use 'sbxgo setup' once to scaffold config and prepare the template image.
Use 'sbxgo run' for everyday work to resume or create the sandbox.`,
		Version:       versionString(),
		SilenceUsage:  true,
		SilenceErrors: false,
	}

	cmd.PersistentFlags().BoolVarP(&debug, "debug", "D", false,
		"Pass --debug to every sbx invocation and include stack traces in error output")

	cmd.AddCommand(newSetupCmd(&debug), newRunCmd(&debug))

	return cmd, &debug
}

func newSetupCmd(debug *bool) *cobra.Command {
	var (
		agent    string
		force    bool
		dryRun   bool
		tomlPath string
	)

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Scaffold config, build Docker template, and create the sandbox",
		Long: `setup reads .sbxgo/config.toml, builds or pulls a Docker template image when
[sandbox.docker] is configured, and creates (or recreates) the project sandbox.

If .sbxgo/config.toml does not exist, setup creates one from a template and exits
so you can edit it before proceeding.

Run this when setting up a project for the first time, or when the Dockerfile or
published image you depend on has changed. For everyday use, run 'sbxgo run' instead.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return sandbox.Setup(cmd.Context(), sandbox.SetupOptions{
				Agent:    agent,
				Force:    force,
				DryRun:   dryRun,
				Debug:    *debug,
				TOMLPath: tomlPath,
			},
				runner.NewReal(),
				fsutil.NewReal(),
				prompt.NewTerminal(),
			)
		},
		SilenceUsage: true,
	}

	cmd.Flags().StringVar(&agent, "agent", "claude", "Agent to use when creating a new config (e.g. claude, codex)")
	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation when recreating an existing sandbox")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print what would happen without executing")
	cmd.Flags().StringVar(&tomlPath, "toml", sandbox.DefaultConfigPath, "Path to config.toml")

	return cmd
}

func newRunCmd(debug *bool) *cobra.Command {
	var (
		dryRun   bool
		tomlPath string
	)

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Resume or create the sandbox for the current project",
		Long: `run reads .sbxgo/config.toml, applies network policies, and either resumes
an existing sandbox or creates a new one with the configured kits.

This is the everyday command. Run 'sbxgo setup' first when setting up a new project
or after changing the Dockerfile / published image.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return sandbox.Start(cmd.Context(), sandbox.StartOptions{
				DryRun:   dryRun,
				Debug:    *debug,
				TOMLPath: tomlPath,
			},
				runner.NewReal(),
				fsutil.NewReal(),
				prompt.NewTerminal(),
			)
		},
		SilenceUsage: true,
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print what would happen without executing")
	cmd.Flags().StringVar(&tomlPath, "toml", sandbox.DefaultConfigPath, "Path to config.toml")

	return cmd
}
