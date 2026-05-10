package sandbox

import (
	_ "embed"
	"path/filepath"
	"strings"

	"github.com/rotisserie/eris"

	"github.com/HenrikPoulsen/sbxgo/internal/fsutil"
)

//go:embed config.toml.tmpl
var configTemplate string

//go:embed gitignore.tmpl
var gitignoreTemplate string

const gitignorePath = ".sbxgo/.gitignore"

// scaffoldConfig creates DefaultConfigPath and .sbxgo/.gitignore from embedded templates
// if the config does not already exist. Returns true if files were created.
func scaffoldConfig(agent string, fs fsutil.FileSystem) (bool, error) {
	exists, err := fs.Exists(DefaultConfigPath)
	if err != nil {
		return false, eris.Wrapf(err, "checking config path %q", DefaultConfigPath)
	}

	if exists {
		return false, nil
	}

	dir := filepath.Dir(DefaultConfigPath)
	if err := fs.MkdirAll(dir, 0o755); err != nil {
		return false, eris.Wrapf(err, "creating directory %q", dir)
	}

	content := strings.ReplaceAll(configTemplate, "{{AGENT}}", agent)

	if err := fs.WriteFile(DefaultConfigPath, []byte(content), 0o644); err != nil {
		return false, eris.Wrapf(err, "writing config %q", DefaultConfigPath)
	}

	giExists, err := fs.Exists(gitignorePath)
	if err != nil {
		return false, eris.Wrapf(err, "checking %q", gitignorePath)
	}

	if !giExists {
		if err := fs.WriteFile(gitignorePath, []byte(gitignoreTemplate), 0o644); err != nil {
			return false, eris.Wrapf(err, "writing %q", gitignorePath)
		}
	}

	return true, nil
}
