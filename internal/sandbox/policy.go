package sandbox

import (
	"context"
	"fmt"
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
// network --sandbox <sandbox> ...` rejects unknown names.
func applyPolicy(
	ctx context.Context,
	client *sbx.Client,
	sandboxName string,
	cfg *config.SandboxConfig,
	dryRun bool,
) error {
	fmt.Printf("Network policy: configured base %q "+
		"(initialize host-wide with `sbx policy init %s`)\n",
		cfg.NetworkPolicy, cfg.NetworkPolicy)

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
