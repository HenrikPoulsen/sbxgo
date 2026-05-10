// Package sandbox contains the high-level orchestration logic for sbxgo run and sbxgo setup.
package sandbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/rotisserie/eris"

	"github.com/HenrikPoulsen/sbxgo/internal/config"
	"github.com/HenrikPoulsen/sbxgo/internal/fsutil"
	"github.com/HenrikPoulsen/sbxgo/internal/sbx"
)

const (
	// DefaultConfigPath is the default location of .sbxgo/config.toml.
	DefaultConfigPath = ".sbxgo/config.toml"
	// ImageIDFile stores the last built Docker image ID.
	ImageIDFile = ".sbxgo/.image-id"
	// ImageIDNewFile is used as a temporary iidfile during docker build.
	ImageIDNewFile = ".sbxgo/.image-id-new"
	// CreateStateFile stores a hash of the create-time configuration so resume
	// can detect drift. Only includes fields that cannot be applied to an
	// existing sandbox (docker source, branch, extra_workspaces).
	CreateStateFile = ".sbxgo/.create-state"
)

// validNameRE matches the character set sbx allows in a sandbox name:
// letters, digits, '.', '+', and '-' (per `sbx create --help`).
var validNameRE = regexp.MustCompile(`^[A-Za-z0-9.+\-]+$`)

// Name returns the sandbox name in the form "{agent}-{project-dirname}".
// It returns an error if the resulting name contains characters sbx rejects
// (e.g., spaces or underscores in the workdir basename).
func Name(agent, workdir string) (string, error) {
	name := agent + "-" + filepath.Base(workdir)

	if !validNameRE.MatchString(name) {
		return "", eris.Errorf(
			"sandbox name %q contains invalid characters; sbx allows only letters, digits, '.', '+', and '-' "+
				"(rename or move the project directory, or pick an agent name without special characters)",
			name)
	}

	return name, nil
}

// WorkDir returns the current working directory or an error.
func WorkDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", eris.Wrap(err, "getting working directory")
	}

	return wd, nil
}

// checkSecrets warns about any required secrets that are not set.
func checkSecrets(ctx context.Context, client *sbx.Client, required []string) error {
	if len(required) == 0 {
		return nil
	}

	existing, err := client.ListSecrets(ctx)
	if err != nil {
		return eris.Wrap(err, "listing secrets")
	}

	existingSet := make(map[string]bool, len(existing))
	for _, s := range existing {
		existingSet[s] = true
	}

	for _, req := range required {
		if !existingSet[req] {
			fmt.Fprintf(os.Stderr, "WARNING: required secret %q is not set (run: sbx secret set %s)\n", req, req)
		}
	}

	return nil
}

// loadConfig reads the TOML config via fs and parses it.
func loadConfig(path string, fs fsutil.FileSystem) (*config.Config, error) {
	data, err := fs.ReadFile(path)
	if err != nil {
		return nil, eris.Wrapf(err, "reading config %q", path)
	}

	cfg, err := config.Parse(data, path)
	if err != nil {
		return nil, eris.Wrapf(err, "parsing config %q", path)
	}

	return cfg, nil
}

// computeCreateStateHash hashes the subset of config that requires a sandbox
// recreate when changed. Returns a hex-encoded SHA-256.
//
// kits are deliberately excluded: resumeSandbox re-applies kits via `sbx kit
// add` on every run, so adding/removing a kit does not require recreating the
// sandbox. (Note that sbx has no `kit rm`, so a kit removed from config stays
// active in the sandbox until it is recreated.)
func computeCreateStateHash(cfg *config.Config, fs fsutil.FileSystem) (string, error) {
	h := sha256.New()

	if _, err := fmt.Fprintf(h, "branch:%s\n", cfg.Sandbox.Branch); err != nil {
		return "", eris.Wrap(err, "hashing branch")
	}

	workspaces := slices.Clone(cfg.Sandbox.ExtraWorkspaces)
	slices.Sort(workspaces)

	for _, ws := range workspaces {
		if _, err := fmt.Fprintf(h, "workspace:%s\n", ws); err != nil {
			return "", eris.Wrap(err, "hashing workspace")
		}
	}

	if err := hashDockerSource(h, cfg.Sandbox.Docker, fs); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// hashDockerSource folds the docker source descriptor into h. For image: the
// ref string is hashed. For build: the dockerfile contents and context path
// are hashed. A nil docker config contributes nothing.
func hashDockerSource(h hash.Hash, dc *config.DockerConfig, fs fsutil.FileSystem) error {
	if dc == nil {
		return nil
	}

	if dc.Image != "" {
		if _, err := fmt.Fprintf(h, "image:%s\n", dc.Image); err != nil {
			return eris.Wrap(err, "hashing image ref")
		}

		return nil
	}

	if dc.Build == nil {
		return nil
	}

	if _, err := fmt.Fprintf(h, "build-context:%s\n", dc.Build.Context); err != nil {
		return eris.Wrap(err, "hashing build context")
	}

	exists, err := fs.Exists(dc.Build.Dockerfile)
	if err != nil {
		return eris.Wrap(err, "checking dockerfile for hash")
	}

	if !exists {
		return nil
	}

	data, err := fs.ReadFile(dc.Build.Dockerfile)
	if err != nil {
		return eris.Wrap(err, "reading dockerfile for hash")
	}

	if _, err := io.WriteString(h, "dockerfile:"); err != nil {
		return eris.Wrap(err, "hashing dockerfile prefix")
	}

	if _, err := h.Write(data); err != nil {
		return eris.Wrap(err, "hashing dockerfile body")
	}

	if _, err := io.WriteString(h, "\n"); err != nil {
		return eris.Wrap(err, "hashing dockerfile terminator")
	}

	return nil
}

// writeCreateState writes the current create-state hash to CreateStateFile.
func writeCreateState(cfg *config.Config, fs fsutil.FileSystem) error {
	hash, err := computeCreateStateHash(cfg, fs)
	if err != nil {
		return err
	}

	if err := fs.WriteFile(CreateStateFile, []byte(hash+"\n"), 0o644); err != nil {
		return eris.Wrap(err, "writing create state")
	}

	return nil
}

// checkDrift returns (drifted, hasState, err). drifted is true if the stored
// create-state hash differs from the current one. hasState is false if no
// state file exists yet (sandbox predates this feature) — the caller should
// then write the current state.
func checkDrift(cfg *config.Config, fs fsutil.FileSystem) (bool, bool, error) {
	hasState, err := fs.Exists(CreateStateFile)
	if err != nil {
		return false, false, eris.Wrap(err, "checking create state")
	}

	if !hasState {
		return false, false, nil
	}

	storedBytes, err := fs.ReadFile(CreateStateFile)
	if err != nil {
		return false, true, eris.Wrap(err, "reading create state")
	}

	current, err := computeCreateStateHash(cfg, fs)
	if err != nil {
		return false, true, err
	}

	stored := strings.TrimSpace(string(storedBytes))

	return stored != current, true, nil
}

// BuildRunArgs constructs the arguments for `sbx run` to create a new sandbox.
// It does NOT prepend "run"; that is handled by the caller.
func BuildRunArgs(cfg *config.SandboxConfig, useTemplate bool, templateName string) []string {
	var args []string
	if useTemplate {
		args = append(args, "--template", templateName)
	}

	for _, kit := range cfg.Kits {
		args = append(args, "--kit", kit)
	}

	if cfg.Branch != "" {
		args = append(args, "--branch", cfg.Branch)
	}

	args = append(args, cfg.Agent, ".")
	args = append(args, cfg.ExtraWorkspaces...)

	return args
}
