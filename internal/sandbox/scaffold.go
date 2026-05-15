package sandbox

import (
	_ "embed"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/rotisserie/eris"

	"github.com/HenrikPoulsen/sbxgo/internal/fsutil"
	"github.com/HenrikPoulsen/sbxgo/internal/prompt"
)

//go:embed config.toml.tmpl
var configTemplate string

//go:embed gitignore.tmpl
var gitignoreTemplate string

const (
	gitignorePath           = ".sbxgo/.gitignore"
	allowedDomainsTokenTmpl = "{{ALLOWED_DOMAINS}}"
)

// scaffoldConfig creates DefaultConfigPath and .sbxgo/.gitignore from embedded templates
// if the config does not already exist. When the agent has known login/API
// domains, it prompts (default yes) to pre-fill allowed_domains so a deny-all
// sandbox is usable out of the box. Returns true if files were created.
func scaffoldConfig(agent string, fs fsutil.FileSystem, p prompt.Prompter) (bool, error) {
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

	allowedBlock, err := resolveAllowedDomainsBlock(agent, p)
	if err != nil {
		return false, err
	}

	content := strings.ReplaceAll(configTemplate, "{{AGENT}}", agent)
	content = strings.ReplaceAll(content, allowedDomainsTokenTmpl, allowedBlock)

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

// resolveAllowedDomainsBlock returns the TOML snippet to substitute for
// {{ALLOWED_DOMAINS}}. If the agent has known login domains, the user is
// prompted (default yes); on yes, the block is pre-filled with those domains
// and a short header comment. Otherwise (or on no), an empty placeholder list
// is returned so the user can fill it in later.
func resolveAllowedDomainsBlock(agent string, p prompt.Prompter) (string, error) {
	domains := loginDomainsFor(agent)
	if len(domains) == 0 {
		return emptyAllowedDomainsBlock(), nil
	}

	question := fmt.Sprintf(
		"Pre-fill allowed_domains with login/API endpoints for %q (%s)?",
		agent, strings.Join(domains, ", "),
	)

	yes, err := p.Confirm(question, true)
	if err != nil {
		return "", eris.Wrap(err, "reading confirmation")
	}

	if !yes {
		return emptyAllowedDomainsBlock(), nil
	}

	return prefilledAllowedDomainsBlock(domains), nil
}

func emptyAllowedDomainsBlock() string {
	return "allowed_domains = [\n  # \"api.example.com\",\n]"
}

func prefilledAllowedDomainsBlock(domains []string) string {
	var b strings.Builder

	b.WriteString("# Login/API endpoints required by the agent. Added by `sbxgo setup`;\n")
	b.WriteString("# safe to remove if you don't need agent login from inside the sandbox.\n")
	b.WriteString("allowed_domains = [\n")

	for _, d := range domains {
		fmt.Fprintf(&b, "  %q,\n", d)
	}

	b.WriteString("]")

	return b.String()
}
