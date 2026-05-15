package sbx

import (
	"context"
	_ "embed"
	"strconv"
	"strings"

	"github.com/rotisserie/eris"
)

//go:embed min-version.txt
var minVersionFile string

// MinVersion is the minimum supported sbx CLI version, embedded from
// min-version.txt at compile time. Bump it by editing that file — keeping
// the value in one place lets docs, scripts, and the binary stay in sync.
//
//nolint:gochecknoglobals // immutable after package init; sourced from embedded file
var MinVersion = strings.TrimSpace(minVersionFile)

// semverComponents is the number of dot-separated parts in MAJOR.MINOR.PATCH.
const semverComponents = 3

// Version returns the parsed client version from `sbx version`, e.g. "0.29.0".
// Any pre-release suffix (e.g. "-rc1") is stripped so RC builds satisfy a
// release-level minimum.
func (c *Client) Version(ctx context.Context) (string, error) {
	out, err := c.outputCmd(ctx, "version")
	if err != nil {
		return "", eris.Wrap(err, "running `sbx version` (is sbx installed and on PATH?)")
	}

	v, err := parseClientVersion(string(out))
	if err != nil {
		return "", eris.Wrapf(err, "parsing `sbx version` output: %q", strings.TrimSpace(string(out)))
	}

	return v, nil
}

// CheckMinVersion fails if sbx is missing, unparseable, or older than MinVersion.
func (c *Client) CheckMinVersion(ctx context.Context) error {
	got, err := c.Version(ctx)
	if err != nil {
		return err
	}

	cmp, err := compareVersions(got, MinVersion)
	if err != nil {
		return eris.Wrapf(err, "comparing sbx version %q to minimum %q", got, MinVersion)
	}

	if cmp < 0 {
		return eris.Errorf(
			"sbx %s is older than the minimum required version %s; "+
				"upgrade from https://github.com/docker/sbx-releases", got, MinVersion)
	}

	return nil
}

// parseClientVersion extracts the X.Y.Z version from `sbx version` output.
// Expected format:
//
//	Client Version:  v0.29.0 <commit-sha>
//	Server Version:  ...
//
// The leading "v" is trimmed; any "-suffix" pre-release tag is dropped.
func parseClientVersion(out string) (string, error) {
	for line := range strings.SplitSeq(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < semverComponents {
			continue
		}

		if !strings.EqualFold(fields[0], "Client") || !strings.HasPrefix(fields[1], "Version") {
			continue
		}

		raw := strings.TrimPrefix(fields[2], "v")
		if i := strings.IndexByte(raw, '-'); i >= 0 {
			raw = raw[:i]
		}

		if raw == "" {
			return "", eris.New("empty version after `Client Version:`")
		}

		return raw, nil
	}

	return "", eris.New("no `Client Version:` line found")
}

// compareVersions returns -1/0/1 for a<b / a==b / a>b on X.Y.Z strings.
// Both inputs must be three dot-separated non-negative integers; pre-release
// suffixes should be stripped before calling.
func compareVersions(a, b string) (int, error) {
	ap, err := parseTriple(a)
	if err != nil {
		return 0, eris.Wrapf(err, "parsing %q", a)
	}

	bp, err := parseTriple(b)
	if err != nil {
		return 0, eris.Wrapf(err, "parsing %q", b)
	}

	for i := range ap {
		if ap[i] != bp[i] {
			if ap[i] < bp[i] {
				return -1, nil
			}

			return 1, nil
		}
	}

	return 0, nil
}

func parseTriple(v string) ([3]int, error) {
	var out [3]int

	parts := strings.Split(v, ".")
	if len(parts) != semverComponents {
		return out, eris.Errorf("expected MAJOR.MINOR.PATCH, got %d parts", len(parts))
	}

	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return out, eris.Wrapf(err, "component %d (%q) is not an integer", i, p)
		}

		if n < 0 {
			return out, eris.Errorf("component %d (%d) is negative", i, n)
		}

		out[i] = n
	}

	return out, nil
}
