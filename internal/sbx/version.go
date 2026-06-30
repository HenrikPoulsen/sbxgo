package sbx

import (
	"context"
	"strconv"
	"strings"

	"github.com/rotisserie/eris"
)

// MinVersion is the lowest sbx client version sbxgo will run against. Bumped
// when sbxgo starts relying on a feature or flag introduced upstream.
const MinVersion = "0.34.0"

// Version returns the sbx client version (without a leading "v"), parsed from
// `sbx version`. The server line is ignored: the daemon may be down at the
// moment sbxgo runs the check, and the client version is what governs CLI
// flag and behaviour compatibility.
func (c *Client) Version(ctx context.Context) (string, error) {
	out, err := c.outputCmd(ctx, "version")
	if err != nil {
		return "", eris.Wrap(err, "sbx version")
	}

	v := parseClientVersion(string(out))
	if v == "" {
		return "", eris.Errorf("could not parse sbx version output: %q", trimForError(string(out)))
	}

	return v, nil
}

// CheckMinVersion verifies that the installed sbx client is at least MinVersion
// and returns an actionable error otherwise. Pre-release versions (e.g. -rc1)
// of MinVersion are rejected as older.
func (c *Client) CheckMinVersion(ctx context.Context) error {
	got, err := c.Version(ctx)
	if err != nil {
		return err
	}

	cmp, err := compareSbxVersions(got, MinVersion)
	if err != nil {
		return eris.Wrapf(err, "comparing sbx version %q to required %q", got, MinVersion)
	}

	if cmp < 0 {
		return eris.Errorf(
			"sbx %s is older than the minimum required by sbxgo (%s); "+
				"upgrade sbx from https://github.com/docker/sbx-releases and try again",
			got, MinVersion,
		)
	}

	return nil
}

// parseClientVersion extracts version (no leading "v") from `sbx version` output.
// Two formats exist; --debug (-D) emits the second:
//
//	sbx version: v0.34.0 <sha>     // default
//	Client Version:  v0.34.0 <sha> // with -D flag
func parseClientVersion(out string) string {
	prefixes := []string{"sbx version:", "Client Version:"}

	for line := range strings.SplitSeq(out, "\n") {
		line = strings.TrimSpace(line)

		for _, prefix := range prefixes {
			rest, ok := strings.CutPrefix(line, prefix)
			if !ok {
				continue
			}

			fields := strings.Fields(rest)
			if len(fields) == 0 {
				return ""
			}

			return strings.TrimPrefix(fields[0], "v")
		}
	}

	return ""
}

// compareSbxVersions returns -1, 0, or +1 comparing two MAJOR.MINOR.PATCH[-PRE]
// strings. A pre-release suffix (anything after the first '-') is considered
// older than the same base version with no suffix. Pre-release suffixes are
// compared lexicographically among themselves, which is sufficient for the
// rc1/rc2/… cadence sbx publishes.
func compareSbxVersions(a, b string) (int, error) {
	aBase, aPre := splitPre(a)
	bBase, bPre := splitPre(b)

	aParts, err := parseBase(aBase)
	if err != nil {
		return 0, eris.Wrapf(err, "parsing %q", a)
	}

	bParts, err := parseBase(bBase)
	if err != nil {
		return 0, eris.Wrapf(err, "parsing %q", b)
	}

	for i := range aParts {
		if aParts[i] != bParts[i] {
			if aParts[i] < bParts[i] {
				return -1, nil
			}

			return 1, nil
		}
	}

	return comparePre(aPre, bPre), nil
}

func splitPre(v string) (string, string) {
	base, pre, _ := strings.Cut(v, "-")
	return base, pre
}

// numSemverComponents is the number of dot-separated parts in the base version
// (MAJOR.MINOR.PATCH) parsed from `sbx version`.
const numSemverComponents = 3

func parseBase(s string) ([numSemverComponents]int, error) {
	var out [numSemverComponents]int

	parts := strings.Split(s, ".")
	if len(parts) != numSemverComponents {
		return out, eris.Errorf("expected MAJOR.MINOR.PATCH, got %q", s)
	}

	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return out, eris.Wrapf(err, "parsing component %q", p)
		}

		out[i] = n
	}

	return out, nil
}

// trimForError caps an unbounded blob (like an entire `sbx version` stdout
// dump) so it does not drown the error message.
func trimForError(s string) string {
	const maxLen = 200

	s = strings.TrimSpace(s)
	if len(s) > maxLen {
		return s[:maxLen] + "…(truncated)"
	}

	return s
}

func comparePre(a, b string) int {
	switch {
	case a == b:
		return 0
	case a == "":
		// No pre-release > any pre-release.
		return 1
	case b == "":
		return -1
	case a < b:
		return -1
	default:
		return 1
	}
}
