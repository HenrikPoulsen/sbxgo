# sbxgo

A single-binary CLI for running AI coding agents in [Docker Sandboxes](https://github.com/docker/sbx-releases). Two subcommands:

- **`sbxgo setup`**: build or pull the Docker template image for your project, create the sandbox, and (after a confirmation prompt) attach to the agent. Run once when setting up, and again when the image source changes.
- **`sbxgo run`**: resume or create the sandbox for the current project, detecting drift in create-time config along the way. Your everyday command.

Inspired by [maxkrivich/sbx-toolkit](https://github.com/maxkrivich/sbx-toolkit). The original project took a machine-level, dotfiles-style approach, baking `~/.claude` into a shared local image used by all projects. `sbxgo` shifts to a project-level model: each project owns its own Dockerfile and config, committed to the repo, so the whole team gets the same environment without any per-machine setup step.

My hope is that `sbx` makes this project redundant over time as they add more functionality.
As of when creating this repo it was a bit of a pain to set up `sbx` on each new repository, especially when running with a deny-all policy for increased security.

So this tooling tries to make it easier to use sbx overall if you use it in a per-repository setup.

---

## Install

### From a release (recommended)

**Linux / macOS:**

```bash
# latest
curl -fsSL https://raw.githubusercontent.com/HenrikPoulsen/sbxgo/main/install.sh | bash

# pin to a specific version
curl -fsSL https://raw.githubusercontent.com/HenrikPoulsen/sbxgo/main/install.sh | bash -s v0.3.0
```

**Windows** (run from `cmd.exe` or PowerShell):

```cmd
:: latest
powershell -c "irm https://raw.githubusercontent.com/HenrikPoulsen/sbxgo/main/install.ps1 | iex"

:: pin to a specific version
powershell -c "$env:SBXGO_VERSION='v0.3.0'; irm https://raw.githubusercontent.com/HenrikPoulsen/sbxgo/main/install.ps1 | iex"
```

Both installers resolve the requested release (or the latest), download the platform-specific binary, and verify its SHA-256 against the release's `checksums.txt`. On mismatch the binary is deleted and the install aborts.

`SBXGO_VERSION` accepts either form (`v0.3.0` or `0.3.0`). The bash script also takes the version as a positional arg (`bash -s v0.3.0`), useful when piping. The Windows installer adds the install directory (`%LOCALAPPDATA%\Programs\sbxgo` by default; override with `$env:SBXGO_INSTALL_DIR`) to your user `PATH` — open a new terminal afterwards to pick it up.

### Via `go install`

If you have Go installed, you can just do this:

```bash
go install github.com/HenrikPoulsen/sbxgo/cmd/sbxgo@latest
```

Pin to a version by replacing `@latest` with the tag (e.g. `@v0.3.0`). The binary lands in `$GOBIN` (or `$GOPATH/bin`); make sure that directory is on your `PATH`.

---

## Quick start

### 1. Scaffold config

From your project repository:

```bash
sbxgo setup
```

If `.sbxgo/config.toml` does not exist, `sbxgo setup` creates one from a template and exits. Open the file and edit it to match your project, then run `sbxgo setup` again.

Commit `.sbxgo/config.toml`. Everyone on the team uses the same settings.

### 2. (Optional) Use a custom template image

If your sandbox needs custom tooling, add `[sandbox.docker]` to `.sbxgo/config.toml`. Pick exactly one source:

**Build from a Dockerfile in your repo:**

```toml
[sandbox.docker.build]
# context defaults to ".", dockerfile defaults to ".sbxgo/Dockerfile"
```

```dockerfile
# .sbxgo/Dockerfile
ARG AGENT=claude-code
FROM docker/sandbox-templates:${AGENT}
ARG AGENT

USER root
RUN apt-get update && apt-get install -y ripgrep && rm -rf /var/lib/apt/lists/*
USER agent
```

**Or pull a pre-published image:**

```toml
[sandbox.docker]
image = "ghcr.io/acme/dev:1.4.0"
```

Re-run `sbxgo setup` after changing the Dockerfile or bumping the image tag. Setup compares the resolved image ID against the last build and only reloads the sbx template when the ID has actually changed; rebuilding the same Dockerfile is fast.

> **Heads up:** kits (see below) are usually a better fit for project-specific tooling than a custom image. Use kits unless you genuinely need a different base or layered system packages.

### 3. Run

From your project repository:

```bash
sbxgo run
```

Resumes the existing sandbox, or creates a new one if it does not exist yet.

---

## `sbxgo run`

```
sbxgo run [flags]

Flags:
  -D, --debug      Pass --debug to every sbx invocation (verbose logging)
      --dry-run    Print what would happen without executing
      --toml PATH  Path to config.toml (default: .sbxgo/config.toml)
```

On each invocation, `sbxgo run`:
1. Applies `allowed_domains` / `denied_domains` from config (the host-wide network policy itself is left alone — see the heads-up box below)
2. Warns about any missing required secrets
3. If the sandbox exists, checks for **drift** in create-time config (docker source, branch, extra_workspaces). On drift, prompts to recreate; if you decline, resumes the existing sandbox with a warning that the new config will not take effect until a recreate
4. Resumes the existing sandbox, re-applying any kits listed in config so kit changes take effect without recreating
5. If no sandbox exists, creates one and attaches to the agent

---

## `sbxgo setup`

```
sbxgo setup [flags]

Flags:
  -D, --debug       Pass --debug to every sbx invocation (verbose logging)
      --agent NAME  Agent to use when scaffolding a new config (default: claude)
      --force       Skip both confirmation prompts (recreate + start-agent)
      --dry-run     Print what would happen without executing
      --toml PATH   Path to config.toml (default: .sbxgo/config.toml)
```

On each invocation, `sbxgo setup`:
1. Applies `allowed_domains` / `denied_domains` from config
2. Warns about any missing required secrets
3. If `[sandbox.docker]` is set, builds (`docker.build`) or pulls (`docker.image`) the source image, then loads it into sbx as a named template — but only when the resolved image ID differs from the last setup
4. If a sandbox already exists for this project, prompts to recreate it (skip with `--force`); on confirm, removes it
5. Creates the sandbox via `sbx create`
6. Prompts "Start the agent now?" (defaults to yes; `--force` skips and attaches automatically). Decline to leave the sandbox dormant — `sbxgo run` later will attach.

---

## Kits

Kits are directories you commit to the repo that install tools and apply configuration inside the sandbox at startup. They are the preferred way to add project-specific tooling without baking a full custom Docker image.

A kit has two files:

```
.sbxgo/kits/my-kit/
  spec.yaml       # metadata and network rules
  files/          # overlaid onto the sandbox filesystem root
```

### Example: install ripgrep and allow github.com

`.sbxgo/kits/tools/spec.yaml`:

```yaml
schemaVersion: "1"
kind: mixin
name: tools
description: Project dev tools

network:
  allowedDomains:
    - github.com

commands:
  install:
    - command: apt-get update -qq
      user: "0"
      description: Refresh apt index
    - command: apt-get install -y --no-install-recommends ripgrep
      user: "0"
      description: Install ripgrep
    - command: rm -rf /var/lib/apt/lists/*
      user: "0"
      description: Clean apt cache
```

Then reference it in `.sbxgo/config.toml`:

```toml
kits = [".sbxgo/kits/tools"]
```

The kit is applied when the sandbox is created (`sbxgo setup` or first `sbxgo run`) and re-applied on every subsequent `sbxgo run` so changes to the kit take effect without recreating the sandbox. Network rules in `spec.yaml` are additive on top of `network_policy`.

> **Heads up:** sbx has no `kit rm`. **Removing** a kit from the `kits = [...]` list in your config does not unapply it — the kit's files and packages stay in the sandbox until it's recreated. Run `sbxgo setup --force` to start fresh after dropping a kit.

For more information and a collection of ready-made kits, see [docker/sbx-kits-contrib](https://github.com/docker/sbx-kits-contrib).

---

## `.sbxgo/config.toml` reference

The template written to `.sbxgo/config.toml` by `sbxgo setup` is [config.toml.tmpl](./internal/sandbox/config.toml.tmpl). The `.sbxgo/.gitignore` template is [gitignore.tmpl](./internal/sandbox/gitignore.tmpl).

| Field | Required | Default | Description |
|---|---|---|---|
| `agent` | yes | | `claude`, `codex`, `kiro`, `shell`, etc. |
| `[sandbox.docker]` | | | Source of the template image. Set exactly one of `image` or `[sandbox.docker.build]`, or omit the section to use sbx's default base. |
| `docker.image` | | | Registry reference, e.g. `ghcr.io/acme/dev:1.4.0`. Pulled by `sbxgo setup`. |
| `docker.build.context` | | `.` | Build context passed to `docker build`. |
| `docker.build.dockerfile` | | `.sbxgo/Dockerfile` | Path to the Dockerfile. |
| `network_policy` | | `deny-all` | `allow-all`, `balanced`, or `deny-all` |
| `branch` | | | `auto`, a branch name, or omit for direct mode |
| `[sandbox.secrets]` | | | Map of sbx service name → env var name. Each entry both declares a required secret (warn-on-missing) and tells sbxgo which env var to read after sandbox creation. Empty env-var value = warn-only, no auto-sync. |
| `allowed_domains` | | | Extra domains to allow on top of the base policy |
| `denied_domains` | | | Domains to deny even if the base policy allows them |
| `kits` | | | Kit references applied to the sandbox; re-applied on each `sbxgo run` |
| `extra_workspaces` | | | Extra host paths to mount into the sandbox |

### Network policy

```
network_policy  (base: allow-all / balanced / deny-all)
      |
      v
allowed_domains (additive)
      |
      v
denied_domains  (always wins)
```

> **Heads up:** `sbx policy` is host-wide today
> ([docker/sbx-releases#91](https://github.com/docker/sbx-releases/issues/91)).
> `sbxgo` won't auto-flip `network_policy` — you'll see a warning if it
> doesn't match the active default. `allowed_domains` / `denied_domains` are
> still applied, but the rules persist across sandboxes and reboots until
> manually removed.

### Secrets

`[sandbox.secrets]` maps each sbx service (`sbx secret set --help` lists them) to the env var that holds its value:

```toml
[sandbox.secrets]
my-service = "MY_SERVICE_API_KEY"
```

After sandbox creation, sbxgo reads each env var and sets it as a per-sandbox secret. Pairs naturally with `op run --env-file=…`, `doppler run`, `direnv`, etc. If the env var is empty and the secret isn't already in sbx, sbxgo warns at startup.

---

## Contributing

Issues and PRs welcome.

PR titles must follow [Conventional Commits](https://www.conventionalcommits.org/) (e.g. `feat: add foo`, `fix: handle nil case`, `feat!: rename Run to Start` for breaking changes). The PR-title check enforces this. The repo is intentionally on `0.x` — `feat!` bumps the minor version, not the major (see `.releaserc.yml`); when we're ready to graduate to 1.0.0 we'll remove that override.

Design goals:
- **Project-level**: config and Dockerfiles live in the repo, not on the developer's machine
- **No runtime dependencies**: single static binary
- **Safe by default**: secrets never touch the filesystem or image layers
- **Testable**: all external calls go through interfaces; no global state

### Release process

Pushes to `main` trigger CI (lint + test) followed by `semantic-release`, which decides the next version from the conventional-commit history, creates the GitHub release with auto-generated notes, and then invokes `goreleaser` via `successCmd` to attach the binaries.

If `goreleaser` fails *after* `semantic-release` has already created the release (build flake, signing issue, transient network), the release will exist with notes but no binary assets. Recovery: re-run the release workflow on the same commit. `goreleaser`'s `release.mode: keep-existing` setting preserves the body and just appends the missing artifacts on the next attempt — no need to delete and re-tag.

---

## License

MIT — see [LICENSE](./LICENSE). Third-party attributions are listed in [THIRD_PARTY_LICENSES.md](./THIRD_PARTY_LICENSES.md).
