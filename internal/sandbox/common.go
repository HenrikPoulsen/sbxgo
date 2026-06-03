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
	// existing sandbox (docker source, clone, extra_workspaces).
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
// Kit contents are included because sbx applies kits at `sbx create` time and
// has no `kit rm`. Re-applying via `sbx kit add` on every resume is unsafe
// (e.g. apt locks during package installs) and silently overlays files, so
// sbxgo treats kit changes as drift instead: the user is prompted to
// recreate, which re-applies the kit cleanly at creation.
func computeCreateStateHash(cfg *config.Config, fs fsutil.FileSystem) (string, error) {
	h := sha256.New()

	if _, err := fmt.Fprintf(h, "clone:%v\n", cfg.Sandbox.Clone); err != nil {
		return "", eris.Wrap(err, "hashing clone")
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

	// Kits are hashed in declared order, since sbx applies them in order
	// and a reorder is a meaningful semantic change (later kits can
	// override earlier ones).
	for _, kit := range cfg.Sandbox.Kits {
		if err := hashKit(h, kit, fs); err != nil {
			return "", err
		}
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

// hashKit folds a kit reference into h. For local-directory kits
// (detected by the presence of spec.yaml inside the path), the full file
// tree is hashed so any edit to spec.yaml or files/ counts as drift. For
// non-directory references (URL, OCI ref, ZIP path), the reference string
// itself is hashed.
func hashKit(h hash.Hash, kit string, fs fsutil.FileSystem) error {
	if _, err := fmt.Fprintf(h, "kit:%s\n", kit); err != nil {
		return eris.Wrap(err, "hashing kit reference")
	}

	hasSpec, err := fs.Exists(filepath.Join(kit, "spec.yaml"))
	if err != nil {
		return eris.Wrapf(err, "checking spec.yaml for kit %q", kit)
	}

	if !hasSpec {
		// Not a local directory we can introspect; the reference string
		// above is the only signal we have.
		return nil
	}

	files, err := fs.WalkFiles(kit)
	if err != nil {
		return eris.Wrapf(err, "walking kit directory %q", kit)
	}

	for _, rel := range files {
		full := filepath.ToSlash(filepath.Join(kit, rel))

		data, err := fs.ReadFile(full)
		if err != nil {
			return eris.Wrapf(err, "reading kit file %q", full)
		}

		if _, err := fmt.Fprintf(h, "file:%s:", rel); err != nil {
			return eris.Wrap(err, "hashing kit file path")
		}

		if _, err := h.Write(data); err != nil {
			return eris.Wrap(err, "hashing kit file body")
		}

		if _, err := io.WriteString(h, "\n"); err != nil {
			return eris.Wrap(err, "hashing kit file terminator")
		}
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
// state file exists yet (sandbox predates this feature); the caller should
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

	if cfg.Clone {
		args = append(args, "--clone")
	}

	args = append(args, cfg.Agent, ".")
	args = append(args, cfg.ExtraWorkspaces...)

	return args
}
