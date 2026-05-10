package sandbox

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/rotisserie/eris"

	"github.com/HenrikPoulsen/sbxgo/internal/config"
	"github.com/HenrikPoulsen/sbxgo/internal/docker"
	"github.com/HenrikPoulsen/sbxgo/internal/fsutil"
	"github.com/HenrikPoulsen/sbxgo/internal/prompt"
	"github.com/HenrikPoulsen/sbxgo/internal/runner"
	"github.com/HenrikPoulsen/sbxgo/internal/sbx"
)

// SetupOptions configures the sbxgo setup command.
type SetupOptions struct {
	Agent    string // agent to use when scaffolding a new config (default: "claude")
	Force    bool   // skip confirmation prompt
	DryRun   bool
	Debug    bool   // pass --debug to every sbx invocation
	TOMLPath string // path to .sbxgo/config.toml, defaults to DefaultConfigPath
}

// Setup runs the full sbxgo setup flow.
func Setup(ctx context.Context, opts SetupOptions, r runner.CommandRunner, fs fsutil.FileSystem, p prompt.Prompter) error {
	if opts.TOMLPath == "" {
		opts.TOMLPath = DefaultConfigPath
	}

	if opts.Agent == "" {
		opts.Agent = "claude"
	}

	done, err := prepareConfig(opts, fs)
	if err != nil || done {
		return err
	}

	cfg, err := loadConfig(opts.TOMLPath, fs)
	if err != nil {
		return err
	}

	sbxClient := sbx.NewClient(r).SetDebug(opts.Debug).SetVerbose(opts.Debug || opts.DryRun)
	dockerClient := docker.NewClient(r)

	wd, err := WorkDir()
	if err != nil {
		return err
	}

	sandboxName, err := Name(cfg.Sandbox.Agent, wd)
	if err != nil {
		return err
	}

	if err := applyPolicy(ctx, sbxClient, &cfg.Sandbox); err != nil {
		return err
	}

	if err := checkSecrets(ctx, sbxClient, sandboxName, cfg.Sandbox.Secrets); err != nil {
		return err
	}

	useTemplate := cfg.Sandbox.Docker != nil
	templateName := sandboxName

	if useTemplate {
		if err := prepareTemplate(ctx, opts.DryRun, cfg.Sandbox.Docker, templateName, fs, dockerClient, sbxClient); err != nil {
			return err
		}
	}

	if opts.DryRun {
		if err := previewExistingSandboxRemoval(ctx, opts, sbxClient, sandboxName); err != nil {
			return err
		}
	} else {
		aborted, err := removeExistingSandbox(ctx, opts, p, sbxClient, sandboxName)
		if err != nil {
			return err
		}

		if aborted {
			return nil
		}
	}

	runArgs := BuildRunArgs(&cfg.Sandbox, useTemplate, templateName)

	if opts.DryRun {
		fmt.Printf("Would run: sbx create %s\n", strings.Join(runArgs, " "))

		err := syncSecretsFromEnv(ctx, sbxClient, sandboxName, cfg.Sandbox.Secrets, true)
		if err != nil {
			return err
		}

		fmt.Printf("Would run: sbx run %s\n", sandboxName)

		return nil
	}

	fmt.Printf("Creating sandbox '%s'\n", sandboxName)

	if err := sbxClient.Create(ctx, runArgs); err != nil {
		return eris.Wrapf(err, "creating sandbox %q", sandboxName)
	}

	if err := writeCreateState(cfg, fs); err != nil {
		return err
	}

	err = syncSecretsFromEnv(ctx, sbxClient, sandboxName, cfg.Sandbox.Secrets, false)
	if err != nil {
		return err
	}

	return maybeAttachAgent(ctx, opts, p, sbxClient, sandboxName)
}

// prepareConfig handles the pre-flight: in dry-run mode, it just checks
// whether config exists and returns (true, nil) if there's nothing more to
// preview; otherwise it scaffolds a default config and returns (true, nil)
// when scaffolding occurred so the caller can exit. Returns (false, nil)
// when Setup should continue with the existing config.
func prepareConfig(opts SetupOptions, fs fsutil.FileSystem) (bool, error) {
	if opts.DryRun {
		fmt.Println("Dry run: no changes will be made")

		exists, err := fs.Exists(opts.TOMLPath)
		if err != nil {
			return false, eris.Wrapf(err, "checking config path %q", opts.TOMLPath)
		}

		if !exists {
			fmt.Printf("Would create %s\n", DefaultConfigPath)

			return true, nil
		}

		return false, nil
	}

	created, err := scaffoldConfig(opts.Agent, fs)
	if err != nil {
		return false, err
	}

	if created {
		fmt.Printf("Created %s\n", DefaultConfigPath)
		fmt.Println("Edit it to configure your sandbox, then run sbxgo setup again.")
		fmt.Println("When run again, setup will apply network policy, check any required secrets, " +
			"optionally build or pull a Docker template image, and create the sandbox.")

		return true, nil
	}

	return false, nil
}

// maybeAttachAgent prompts the user to attach the agent (unless --force was
// set, in which case it attaches unconditionally) and runs sbx run if so.
func maybeAttachAgent(
	ctx context.Context,
	opts SetupOptions,
	p prompt.Prompter,
	sbxClient *sbx.Client,
	sandboxName string,
) error {
	if !opts.Force {
		start, err := p.Confirm("Sandbox created. Start the agent now? (this will clear the terminal)", true)
		if err != nil {
			return eris.Wrap(err, "reading confirmation")
		}

		if !start {
			fmt.Printf("Run 'sbxgo run' to attach to the sandbox.\n")

			return nil
		}
	}

	if err := sbxClient.Run(ctx, sandboxName); err != nil {
		return eris.Wrapf(err, "starting agent in sandbox %q", sandboxName)
	}

	return nil
}

// previewExistingSandboxRemoval prints what removeExistingSandbox would do
// without touching the sandbox or prompting the user. Used by dry-run mode so
// the user can see the destructive `sbx rm --force` step that setup would
// otherwise perform silently.
func previewExistingSandboxRemoval(
	ctx context.Context,
	opts SetupOptions,
	sbxClient *sbx.Client,
	sandboxName string,
) error {
	exists, err := sbxClient.Exists(ctx, sandboxName)
	if err != nil {
		return eris.Wrapf(err, "checking sandbox %q existence", sandboxName)
	}

	if !exists {
		return nil
	}

	if opts.Force {
		fmt.Printf("Would run: sbx rm --force %s (--force is set)\n", sandboxName)
		return nil
	}

	fmt.Printf("Would prompt: 'Sandbox %q already exists. Recreate it?' (use --force to skip the prompt)\n", sandboxName)
	fmt.Printf("Would run: sbx rm --force %s (if recreate is confirmed)\n", sandboxName)

	return nil
}

// removeExistingSandbox removes an existing sandbox, prompting the user unless Force is set.
// Returns true if the user chose to abort.
func removeExistingSandbox(
	ctx context.Context,
	opts SetupOptions,
	p prompt.Prompter,
	sbxClient *sbx.Client,
	sandboxName string,
) (bool, error) {
	exists, err := sbxClient.Exists(ctx, sandboxName)
	if err != nil {
		return false, eris.Wrapf(err, "checking sandbox %q existence", sandboxName)
	}

	if !exists {
		return false, nil
	}

	if opts.Force {
		fmt.Printf("Removing existing sandbox '%s'\n", sandboxName)

		if err := sbxClient.Remove(ctx, sandboxName); err != nil {
			return false, eris.Wrapf(err, "removing sandbox %q", sandboxName)
		}

		return false, nil
	}

	question := fmt.Sprintf("Sandbox '%s' already exists. Recreate it? This cannot be undone.", sandboxName)

	confirmed, err := p.Confirm(question, false)
	if err != nil {
		return false, eris.Wrap(err, "reading confirmation")
	}

	if !confirmed {
		fmt.Fprintln(os.Stderr, "Aborted.")
		return true, nil
	}

	if err := sbxClient.Remove(ctx, sandboxName); err != nil {
		return false, eris.Wrapf(err, "removing sandbox %q", sandboxName)
	}

	return false, nil
}

// prepareTemplate either builds (build:) or pulls (image:) the source image,
// then loads it as an sbx template if its image ID has changed since last setup.
func prepareTemplate(
	ctx context.Context,
	dryRun bool,
	dockerCfg *config.DockerConfig,
	templateName string,
	fs fsutil.FileSystem,
	dockerClient *docker.Client,
	sbxClient *sbx.Client,
) error {
	newID, err := resolveSourceImage(ctx, dryRun, dockerCfg, templateName, fs, dockerClient)
	if err != nil {
		return err
	}

	if dryRun {
		fmt.Println("Would compare image ID against the stored one and, if changed, docker save + sbx template load.")
		return nil
	}

	storedID, err := readStoredImageID(fs)
	if err != nil {
		return err
	}

	if newID == storedID {
		fmt.Println("Image unchanged, skipping template reload")
		return nil
	}

	fmt.Println("Image changed, loading template...")

	return loadNewTemplate(ctx, newID, templateName, fs, dockerClient, sbxClient)
}

// readStoredImageID returns the previously recorded template image ID, or ""
// if no ImageIDFile exists yet (first setup). A genuine read error is
// surfaced rather than silently treated as a missing file.
func readStoredImageID(fs fsutil.FileSystem) (string, error) {
	exists, err := fs.Exists(ImageIDFile)
	if err != nil {
		return "", eris.Wrap(err, "checking stored image ID")
	}

	if !exists {
		return "", nil
	}

	data, err := fs.ReadFile(ImageIDFile)
	if err != nil {
		return "", eris.Wrap(err, "reading stored image ID")
	}

	return strings.TrimSpace(string(data)), nil
}

// resolveSourceImage runs the build or pull and returns the local image ID
// of the resulting tag. In dry-run mode it prints what would run and returns "".
func resolveSourceImage(
	ctx context.Context,
	dryRun bool,
	dockerCfg *config.DockerConfig,
	templateName string,
	fs fsutil.FileSystem,
	dockerClient *docker.Client,
) (string, error) {
	if dockerCfg.Build != nil {
		if dryRun {
			fmt.Printf("Would run: docker build --iidfile %s -t %s -f %s %s\n",
				ImageIDNewFile, templateName, dockerCfg.Build.Dockerfile, dockerCfg.Build.Context)

			return "", nil
		}

		fmt.Printf("Building Docker image for template '%s'\n", templateName)

		err := dockerClient.Build(ctx, ImageIDNewFile, templateName, dockerCfg.Build.Dockerfile, dockerCfg.Build.Context)
		if err != nil {
			return "", eris.Wrapf(err, "building template %q", templateName)
		}

		return readImageID(fs, ImageIDNewFile)
	}

	if dryRun {
		fmt.Printf("Would run: docker pull %s\n", dockerCfg.Image)
		fmt.Printf("Would run: docker tag %s %s\n", dockerCfg.Image, templateName)

		return "", nil
	}

	fmt.Printf("Pulling Docker image '%s'\n", dockerCfg.Image)

	if err := dockerClient.Pull(ctx, dockerCfg.Image); err != nil {
		return "", eris.Wrapf(err, "pulling image %q for template %q", dockerCfg.Image, templateName)
	}

	if err := dockerClient.Tag(ctx, dockerCfg.Image, templateName); err != nil {
		return "", eris.Wrapf(err, "tagging %q as template %q", dockerCfg.Image, templateName)
	}

	id, err := dockerClient.InspectID(ctx, templateName)
	if err != nil {
		return "", eris.Wrapf(err, "inspecting template %q", templateName)
	}

	return id, nil
}

// readImageID reads an image ID file and trims whitespace.
func readImageID(fs fsutil.FileSystem, path string) (string, error) {
	data, err := fs.ReadFile(path)
	if err != nil {
		return "", eris.Wrapf(err, "reading image ID file %q", path)
	}

	return strings.TrimSpace(string(data)), nil
}

// loadNewTemplate saves the Docker image to a temp tar, loads it as an sbx template,
// and records the new image ID.
func loadNewTemplate(
	ctx context.Context,
	newID, templateName string,
	fs fsutil.FileSystem,
	dockerClient *docker.Client,
	sbxClient *sbx.Client,
) error {
	tmpFile, err := os.CreateTemp("", "sbx-template-*.tar")
	if err != nil {
		return eris.Wrap(err, "creating temp file for template export")
	}

	tmpPath := tmpFile.Name()

	if err := tmpFile.Close(); err != nil {
		return eris.Wrapf(err, "closing temp file %q", tmpPath)
	}

	defer os.Remove(tmpPath) //nolint:errcheck

	if err := dockerClient.Save(ctx, templateName, tmpPath); err != nil {
		return eris.Wrapf(err, "exporting image %q to %q", templateName, tmpPath)
	}

	if err := sbxClient.LoadTemplate(ctx, tmpPath); err != nil {
		return eris.Wrapf(err, "loading sbx template from %q", tmpPath)
	}

	if err := fs.WriteFile(ImageIDFile, []byte(newID+"\n"), 0o644); err != nil {
		return eris.Wrapf(err, "writing image ID to %q", ImageIDFile)
	}

	return nil
}
