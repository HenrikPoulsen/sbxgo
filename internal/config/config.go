// Package config handles loading and validating .sbxgo/config.toml.
package config

import (
	"github.com/BurntSushi/toml"
	"github.com/rotisserie/eris"
)

// NetworkPolicy represents the sbx CLI network policy name used in config.toml.
type NetworkPolicy string

// Network policy constants match the official sbx CLI policy names.
const (
	PolicyAllowAll NetworkPolicy = "allow-all"
	PolicyBalanced NetworkPolicy = "balanced"
	PolicyDenyAll  NetworkPolicy = "deny-all"
)

// DefaultDockerfile is the dockerfile path used when [sandbox.docker.build] is set
// without an explicit dockerfile.
const DefaultDockerfile = ".sbxgo/Dockerfile"

// Config holds the parsed contents of .sbxgo/config.toml.
type Config struct {
	Sandbox SandboxConfig `toml:"sandbox"`
}

// SandboxConfig holds the [sandbox] section of config.toml.
type SandboxConfig struct {
	Agent           string        `toml:"agent"`
	Docker          *DockerConfig `toml:"docker"`
	NetworkPolicy   NetworkPolicy `toml:"network_policy"`
	Branch          string        `toml:"branch"`
	AllowedDomains  []string      `toml:"allowed_domains"`
	DeniedDomains   []string      `toml:"denied_domains"`
	Kits            []string      `toml:"kits"`
	RequiredSecrets []string      `toml:"required_secrets"`
	ExtraWorkspaces []string      `toml:"extra_workspaces"`
}

// DockerConfig holds the [sandbox.docker] section. Exactly one of Image or
// Build must be set.
type DockerConfig struct {
	Image string             `toml:"image"`
	Build *DockerBuildConfig `toml:"build"`
}

// DockerBuildConfig holds the [sandbox.docker.build] table.
type DockerBuildConfig struct {
	Context    string `toml:"context"`
	Dockerfile string `toml:"dockerfile"`
}

// Load reads and parses a TOML config file from the given path using the OS filesystem.
func Load(path string) (*Config, error) {
	var cfg Config

	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, eris.Wrapf(err, "loading config %q", path)
	}

	if err := cfg.Validate(); err != nil {
		return nil, eris.Wrapf(err, "validating config %q", path)
	}

	return &cfg, nil
}

// Parse decodes a TOML config from raw bytes. path is used only for error messages.
func Parse(data []byte, path string) (*Config, error) {
	var cfg Config

	if _, err := toml.Decode(string(data), &cfg); err != nil {
		return nil, eris.Wrapf(err, "loading config %q", path)
	}

	if err := cfg.Validate(); err != nil {
		return nil, eris.Wrapf(err, "validating config %q", path)
	}

	return &cfg, nil
}

// Validate checks that required fields are present and values are valid.
func (c *Config) Validate() error {
	if c.Sandbox.Agent == "" {
		return eris.New("sandbox.agent is required")
	}

	if c.Sandbox.NetworkPolicy == "" {
		c.Sandbox.NetworkPolicy = PolicyDenyAll
	}

	switch c.Sandbox.NetworkPolicy {
	case PolicyAllowAll, PolicyBalanced, PolicyDenyAll:
	default:
		return eris.Errorf("unknown network_policy %q: must be allow-all, balanced, or deny-all", c.Sandbox.NetworkPolicy)
	}

	if c.Sandbox.Docker != nil {
		if err := validateDocker(c.Sandbox.Docker); err != nil {
			return err
		}
	}

	return nil
}

func validateDocker(d *DockerConfig) error {
	hasImage := d.Image != ""
	hasBuild := d.Build != nil

	switch {
	case hasImage && hasBuild:
		return eris.New("sandbox.docker: set exactly one of image or build, not both")
	case !hasImage && !hasBuild:
		return eris.New("sandbox.docker: set exactly one of image or build")
	}

	if hasBuild {
		if d.Build.Context == "" {
			d.Build.Context = "."
		}

		if d.Build.Dockerfile == "" {
			d.Build.Dockerfile = DefaultDockerfile
		}
	}

	return nil
}
