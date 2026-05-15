package sandbox

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/rotisserie/eris"

	"github.com/HenrikPoulsen/sbxgo/internal/config"
	"github.com/HenrikPoulsen/sbxgo/internal/sbx"
)

// applyPolicy applies the configured allow/deny domain rules to the sandbox.
//
// The flow is list-then-diff:
//  1. ListSandboxRules returns every allow/deny rule that already applies to
//     the sandbox (global "local" rules plus sandbox-scoped ones).
//  2. We diff the configured domains against what's already in place.
//  3. Only the missing entries get sent to `sbx policy allow|deny network`.
//
// This keeps the user-facing log quiet on the common no-op case ("all in
// place") and lets sbx print its own per-resource confirmation when there
// is real work to do. The sandbox must already exist, since `sbx policy allow
// network <sandbox> ...` rejects unknown names.
func applyPolicy(
	ctx context.Context,
	client *sbx.Client,
	sandboxName string,
	cfg *config.SandboxConfig,
	dryRun bool,
) error {
	fmt.Printf("Network policy: configured base %q "+
		"(set host-wide with `sbx policy set-default %s`)\n",
		cfg.NetworkPolicy, cfg.NetworkPolicy)

	warnIfHostDefaultDiffers(ctx, client, string(cfg.NetworkPolicy))

	if len(cfg.AllowedDomains) == 0 && len(cfg.DeniedDomains) == 0 {
		return nil
	}

	if dryRun {
		if len(cfg.AllowedDomains) > 0 {
			fmt.Printf("Would allow for %s: %s\n",
				sandboxName, strings.Join(cfg.AllowedDomains, ", "))
		}

		if len(cfg.DeniedDomains) > 0 {
			fmt.Printf("Would deny for %s: %s\n",
				sandboxName, strings.Join(cfg.DeniedDomains, ", "))
		}

		return nil
	}

	existing, err := client.ListSandboxRules(ctx, sandboxName)
	if err != nil {
		return eris.Wrapf(err, "listing existing policy rules for %q", sandboxName)
	}

	allowToAdd := diffRules(cfg.AllowedDomains, existing, "allow")
	denyToAdd := diffRules(cfg.DeniedDomains, existing, "deny")

	if len(allowToAdd) == 0 && len(denyToAdd) == 0 {
		fmt.Printf("Network rules for %s: all in place\n", sandboxName)
		return nil
	}

	if len(allowToAdd) > 0 {
		if err := client.AllowNetwork(ctx, sandboxName, allowToAdd...); err != nil {
			return eris.Wrapf(err, "allowing domains %v for %q", allowToAdd, sandboxName)
		}

		fmt.Printf("Allow rules for %s: %d added, %d already in place\n",
			sandboxName, len(allowToAdd), len(cfg.AllowedDomains)-len(allowToAdd))
	}

	if len(denyToAdd) > 0 {
		if err := client.DenyNetwork(ctx, sandboxName, denyToAdd...); err != nil {
			return eris.Wrapf(err, "denying domains %v for %q", denyToAdd, sandboxName)
		}

		fmt.Printf("Deny rules for %s: %d added, %d already in place\n",
			sandboxName, len(denyToAdd), len(cfg.DeniedDomains)-len(denyToAdd))
	}

	return nil
}

// diffRules returns the configured resources that do not already appear among
// existing rules of the given decision ("allow" or "deny"). Order from
// configured is preserved so the user sees rules added in the order they
// declared them.
func diffRules(configured []string, existing []sbx.PolicyRule, decision string) []string {
	if len(configured) == 0 {
		return nil
	}

	present := make(map[string]struct{}, len(existing))

	for _, r := range existing {
		if r.Decision == decision {
			present[r.Resource] = struct{}{}
		}
	}

	missing := make([]string, 0, len(configured))

	for _, c := range configured {
		if _, ok := present[c]; !ok {
			missing = append(missing, c)
		}
	}

	return missing
}

// warnIfHostDefaultDiffers prints a WARNING (to stderr) when the host-wide
// default network policy differs from the one configured for this project.
// sbxgo never changes the host default (that's a user choice), but a
// mismatch is almost always a misconfiguration worth surfacing.
//
// The check is best-effort: when the active default cannot be parsed from
// `sbx policy ls --type network` (issue #126), we stay silent rather than
// nag with an "unknown" line. Errors are swallowed: an advisory warning
// failing to fire must not block the run.
func warnIfHostDefaultDiffers(ctx context.Context, client *sbx.Client, desired string) {
	current, err := client.CurrentPolicy(ctx)
	if err != nil || current == "" || current == desired {
		return
	}

	fmt.Fprintf(os.Stderr,
		"WARNING: network_policy is %q but the host-wide default is %q.\n"+
			"         sbxgo does not change the host default automatically. To change it:\n"+
			"             sbx policy set-default %s\n"+
			"         (use `sbx policy reset` first if you want a clean slate; it wipes\n"+
			"         every rule, global AND sandbox-scoped, across all sandboxes.)\n",
		desired, current, desired)
}
