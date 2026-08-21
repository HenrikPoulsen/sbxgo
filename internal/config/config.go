// Package config handles loading and validating .sbxgo/config.toml.
package config

import (
	"os"
	"reflect"
	"strings"

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
	Clone           bool          `toml:"clone"`
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
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, eris.Wrapf(err, "loading config %q", path)
	}

	return Parse(data, path)
}

// Parse decodes a TOML config from raw bytes. path is used only for error messages.
func Parse(data []byte, path string) (*Config, error) {
	var cfg Config

	md, err := toml.Decode(string(data), &cfg)
	if err != nil {
		return nil, eris.Wrapf(err, "loading config %q", path)
	}

	if err := checkUnknownKeys(md); err != nil {
		return nil, eris.Wrapf(err, "validating config %q", path)
	}

	if err := cfg.Validate(); err != nil {
		return nil, eris.Wrapf(err, "validating config %q", path)
	}

	return &cfg, nil
}

// sandboxKeys are the keys valid directly under [sandbox], derived from
// SandboxConfig's toml tags so the misplaced-key hint below cannot drift
// out of sync with the struct. Sub-table fields (struct pointers) are
// skipped: they open their own key namespace and cannot be "swallowed".
var sandboxKeys = sandboxKeyTags()

func sandboxKeyTags() map[string]bool {
	keys := make(map[string]bool)
	t := reflect.TypeOf(SandboxConfig{})

	for i := range t.NumField() {
		field := t.Field(i)
		if field.Type.Kind() == reflect.Pointer {
			continue
		}

		if tag := field.Tag.Get("toml"); tag != "" {
			keys[tag] = true
		}
	}

	return keys
}

// removedKeys maps config keys that existed in earlier sbxgo releases to
// migration guidance, so a committed old config fails with instructions
// instead of a bare "unknown key".
var removedKeys = map[string]string{
	"sandbox.branch": "the branch field was removed; use clone = true (sbx 0.31.0+) instead",
}

// checkUnknownKeys rejects any key in the TOML document that did not decode
// into a Config field. Without this, a misplaced or misspelled key is
// silently ignored — e.g. allowed_domains landing under [sandbox.docker]
// would quietly disable the network allow list.
func checkUnknownKeys(md toml.MetaData) error {
	undecoded := md.Undecoded()
	if len(undecoded) == 0 {
		return nil
	}

	names := make([]string, 0, len(undecoded))
	hint := ""

	for _, key := range undecoded {
		full := key.String()
		names = append(names, full)

		if hint != "" {
			continue
		}

		segments := []string(key)
		last := segments[len(segments)-1]

		switch {
		case removedKeys[full] != "":
			hint = "; " + removedKeys[full]
		case len(segments) == 1 && sandboxKeys[last]:
			hint = "; " + last + " is a [sandbox] key but appears at the top level — did you forget the [sandbox] header?"
		case len(segments) > 1 && segments[0] == "sandbox" && sandboxKeys[last]:
			hint = "; " + last + " belongs directly under [sandbox] — in TOML every key after a " +
				"[table] header joins that table, so move it above the [" +
				strings.Join(segments[:len(segments)-1], ".") +
				"] header (and keep that section at the end of the file)"
		}
	}

	return eris.Errorf("unknown key(s): %s%s", strings.Join(names, ", "), hint)
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
