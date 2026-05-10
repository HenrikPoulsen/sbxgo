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

// applyPolicy applies the network policy and domain rules from config.
//
// We don't auto-flip the host-wide default; `sbx policy` is global today.
// See https://github.com/docker/sbx-releases/issues/91.
func applyPolicy(ctx context.Context, client *sbx.Client, cfg *config.SandboxConfig) error {
	desired := string(cfg.NetworkPolicy)

	current, err := client.CurrentPolicy(ctx)
	if err != nil {
		return eris.Wrap(err, "getting current policy")
	}

	switch current {
	case "":
		// https://github.com/docker/sbx-releases/issues/126
		fmt.Printf("Network policy: configured %q "+
			"(active default unknown; see https://github.com/docker/sbx-releases/issues/126)\n", desired)
	case desired:
		fmt.Printf("Network policy: %s (already set)\n", cfg.NetworkPolicy)
	default:
		fmt.Fprintf(os.Stderr,
			"WARNING: network_policy is %q but the active host-wide default is %q.\n"+
				"         sbxgo does not change the global default automatically. To change it:\n"+
				"             sbx policy reset && sbx policy set-default %s\n"+
				"         `sbx policy reset` wipes every allow/deny rule across all sandboxes.\n",
			desired, current, desired)
	}

	if len(cfg.AllowedDomains) > 0 {
		fmt.Printf("Allowing network: %s\n", strings.Join(cfg.AllowedDomains, ", "))

		if err := client.AllowNetwork(ctx, cfg.AllowedDomains...); err != nil {
			return eris.Wrapf(err, "allowing domains %v", cfg.AllowedDomains)
		}
	}

	if len(cfg.DeniedDomains) > 0 {
		fmt.Printf("Denying network: %s\n", strings.Join(cfg.DeniedDomains, ", "))

		if err := client.DenyNetwork(ctx, cfg.DeniedDomains...); err != nil {
			return eris.Wrapf(err, "denying domains %v", cfg.DeniedDomains)
		}
	}

	return nil
}
