package sandbox

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/rotisserie/eris"

	"github.com/HenrikPoulsen/sbxgo/internal/config"
	"github.com/HenrikPoulsen/sbxgo/internal/fsutil"
	"github.com/HenrikPoulsen/sbxgo/internal/prompt"
	"github.com/HenrikPoulsen/sbxgo/internal/runner"
	"github.com/HenrikPoulsen/sbxgo/internal/sbx"
)

// StartOptions configures the sbxgo run command.
type StartOptions struct {
	DryRun   bool
	Debug    bool   // pass --debug to every sbx invocation
	TOMLPath string // defaults to DefaultConfigPath
}

// Start runs the full sbxgo run flow.
func Start(
	ctx context.Context,
	opts StartOptions,
	r runner.CommandRunner,
	fs fsutil.FileSystem,
	p prompt.Prompter,
) error {
	if opts.TOMLPath == "" {
		opts.TOMLPath = DefaultConfigPath
	}

	if opts.DryRun {
		fmt.Println("Dry run: no changes will be made")
	}

	exists, err := fs.Exists(opts.TOMLPath)
	if err != nil {
		return eris.Wrapf(err, "checking config at %q", opts.TOMLPath)
	}

	if !exists {
		return eris.Errorf("no config found at %q: run `sbxgo setup` to create one", opts.TOMLPath)
	}

	cfg, err := loadConfig(opts.TOMLPath, fs)
	if err != nil {
		return err
	}

	sbxClient := sbx.NewClient(r).SetDebug(opts.Debug).SetVerbose(opts.Debug || opts.DryRun)

	wd, err := WorkDir()
	if err != nil {
		return err
	}

	sandboxName, err := Name(cfg.Sandbox.Agent, wd)
	if err != nil {
		return err
	}

	if err := checkSecrets(ctx, sbxClient, cfg.Sandbox.RequiredSecrets); err != nil {
		return err
	}

	var sandboxExists bool

	sandboxExists, err = sbxClient.Exists(ctx, sandboxName)
	if err != nil {
		return eris.Wrapf(err, "checking sandbox %q existence", sandboxName)
	}

	if sandboxExists {
		recreate, err := handleDrift(opts, cfg, fs, p)
		if err != nil {
			return err
		}

		if recreate {
			fmt.Printf("Removing existing sandbox '%s' to recreate\n", sandboxName)

			if err := sbxClient.Remove(ctx, sandboxName); err != nil {
				return eris.Wrapf(err, "removing sandbox %q for recreate", sandboxName)
			}

			return createSandbox(ctx, opts, cfg, fs, sbxClient, sandboxName)
		}

		return resumeSandbox(ctx, opts, cfg, sbxClient, sandboxName)
	}

	return createSandbox(ctx, opts, cfg, fs, sbxClient, sandboxName)
}

// handleDrift checks whether create-time configuration has changed since the
// sandbox was created and, if so, prompts the user to recreate. Returns true
// if the user confirmed recreation. In dry-run mode, drift is reported but
// no recreate is performed.
func handleDrift(opts StartOptions, cfg *config.Config, fs fsutil.FileSystem, p prompt.Prompter) (bool, error) {
	drifted, hasState, err := checkDrift(cfg, fs)
	if err != nil {
		return false, err
	}

	if !hasState {
		// Sandbox predates drift detection — record current state silently
		// so future changes are detected. Skip on dry-run.
		if !opts.DryRun {
			if err := writeCreateState(cfg, fs); err != nil {
				return false, err
			}
		}

		return false, nil
	}

	if !drifted {
		return false, nil
	}

	hasDocker := cfg.Sandbox.Docker != nil

	if opts.DryRun {
		fmt.Println("Configuration drift detected (docker source, branch, or extra_workspaces changed); " +
			"would prompt to recreate sandbox.")

		return false, nil
	}

	fmt.Println("Configuration affecting sandbox creation has changed since this sandbox was created.")
	fmt.Println("(Affected fields: docker source, branch, extra_workspaces.)")

	if hasDocker {
		fmt.Println("NOTE: a docker source is configured. If the image or Dockerfile changed, run `sbxgo setup` " +
			"instead — `sbxgo run` will recreate the sandbox using the previously loaded template and " +
			"will not rebuild or re-pull.")
	}

	confirmed, err := p.Confirm("Recreate sandbox now? This discards any in-sandbox state.", false)
	if err != nil {
		return false, eris.Wrap(err, "reading confirmation")
	}

	if !confirmed {
		fmt.Fprintln(os.Stderr, "WARNING: continuing with existing sandbox; new configuration will not take effect until recreated.")
		return false, nil
	}

	return true, nil
}

// resumeSandbox resumes an existing sandbox, re-applying configured policy
// rules and kits first so config.toml changes take effect without recreating
// the sandbox. Prints a dry-run message if applicable.
func resumeSandbox(
	ctx context.Context,
	opts StartOptions,
	cfg *config.Config,
	sbxClient *sbx.Client,
	sandboxName string,
) error {
	if opts.DryRun {
		fmt.Printf("Would resume sandbox '%s'\n", sandboxName)
	} else {
		fmt.Printf("Resuming sandbox '%s'\n", sandboxName)
	}

	if err := applyPolicy(ctx, sbxClient, sandboxName, &cfg.Sandbox, opts.DryRun); err != nil {
		return err
	}

	for _, kit := range cfg.Sandbox.Kits {
		if opts.DryRun {
			fmt.Printf("Would run: sbx kit add %s %s\n", sandboxName, kit)
			continue
		}

		fmt.Printf("Applying kit '%s'\n", kit)

		if err := sbxClient.AddKit(ctx, sandboxName, kit); err != nil {
			return eris.Wrapf(err, "applying kit %q to sandbox %q", kit, sandboxName)
		}
	}

	if opts.DryRun {
		fmt.Printf("Would run: sbx run %s\n", sandboxName)
		return nil
	}

	if err := sbxClient.Run(ctx, sandboxName); err != nil {
		return eris.Wrapf(err, "resuming sandbox %q", sandboxName)
	}

	return nil
}

// createSandbox creates a new sandbox from config, warning if the template is not built.
func createSandbox(
	ctx context.Context,
	opts StartOptions,
	cfg *config.Config,
	fs fsutil.FileSystem,
	sbxClient *sbx.Client,
	sandboxName string,
) error {
	if opts.DryRun {
		fmt.Println("No existing sandbox found; would create one.")
	} else {
		fmt.Println("No existing sandbox found, creating...")
	}

	imageIDExists, err := fs.Exists(ImageIDFile)
	if err != nil {
		return eris.Wrapf(err, "checking image ID file %q", ImageIDFile)
	}

	hasDockerConfig := cfg.Sandbox.Docker != nil

	if hasDockerConfig && !imageIDExists {
		fmt.Fprintln(os.Stderr, "WARNING: docker source configured but template not loaded. Run sbxgo setup first.")
	}

	runArgs := BuildRunArgs(&cfg.Sandbox, imageIDExists, sandboxName)

	if opts.DryRun {
		fmt.Printf("Would run: sbx create %s\n", strings.Join(runArgs, " "))

		if err := applyPolicy(ctx, sbxClient, sandboxName, &cfg.Sandbox, true); err != nil {
			return err
		}

		fmt.Printf("Would run: sbx run %s\n", sandboxName)

		return nil
	}

	if err := sbxClient.Create(ctx, runArgs); err != nil {
		return eris.Wrapf(err, "creating sandbox %q", sandboxName)
	}

	if err := writeCreateState(cfg, fs); err != nil {
		return err
	}

	if err := applyPolicy(ctx, sbxClient, sandboxName, &cfg.Sandbox, false); err != nil {
		return err
	}

	if err := sbxClient.Run(ctx, sandboxName); err != nil {
		return eris.Wrapf(err, "starting agent in sandbox %q", sandboxName)
	}

	return nil
}
