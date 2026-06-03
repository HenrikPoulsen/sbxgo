# sbxgo

A single-binary CLI for running AI coding agents in [Docker Sandboxes](https://github.com/docker/sbx-releases). Two subcommands:

- **`sbxgo setup`**: build or pull the Docker template image for your project, create the sandbox, and (after a confirmation prompt) attach to the agent. Run once when setting up, and again when the image source changes.
- **`sbxgo run`**: resume or create the sandbox for the current project, detecting drift in create-time config along the way. Your everyday command.

Inspired by [maxkrivich/sbx-toolkit](https://github.com/maxkrivich/sbx-toolkit). `sbxgo` shifts to a project-level model: each project owns its own Dockerfile and config, committed to the repo, so the whole team gets the same environment without any per-machine setup step.

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
[sandbox]
agent = "claude"

[sandbox.docker.build]
# Both fields are optional and shown here with their defaults.
context    = "."
dockerfile = ".sbxgo/Dockerfile"
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
[sandbox]
agent = "claude"

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
1. Warns about any missing required secrets
2. If the sandbox exists, checks for **drift** in create-time config (docker source, clone, extra_workspaces). On drift, prompts to recreate; if you decline, resumes the existing sandbox with a warning that the new config will not take effect until a recreate
3. Resumes the existing sandbox, applying any `allowed_domains` / `denied_domains` rules that are not already in place. Kits are not re-applied on resume; kit content changes are caught by drift detection (step 2) instead
4. If no sandbox exists, creates one via `sbx create` (which applies all configured kits), then applies sandbox-scoped policy rules, then attaches to the agent

The host-wide network policy is never modified by sbxgo, see the heads-up box below.

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
1. Warns about any missing required secrets
2. If `[sandbox.docker]` is set, builds (`docker.build`) or pulls (`docker.image`) the source image, then loads it into sbx as a named template, but only when the resolved image ID differs from the last setup
3. If a sandbox already exists for this project, prompts to recreate it (skip with `--force`); on confirm, removes it
4. Creates the sandbox via `sbx create`
5. Applies `allowed_domains` / `denied_domains` from config as sandbox-scoped rules
6. Prompts "Start the agent now?" (defaults to yes; `--force` skips and attaches automatically). Decline to leave the sandbox dormant; `sbxgo run` later will attach.

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

Kits are applied **only when the sandbox is created** (`sbxgo setup` or first `sbxgo run`). Network rules in `spec.yaml` are additive on top of `network_policy`.

Changing a kit's `spec.yaml` or `files/` between runs counts as configuration drift: the next `sbxgo run` detects the change and prompts to recreate the sandbox. Re-applying a kit in a live sandbox is unsafe in practice (`apt update` races against existing locks, file overlays silently clobber in-sandbox state), so sbxgo deliberately refuses to do it.

> **Heads up:** sbx has no `kit rm`. **Removing** a kit from the `kits = [...]` list also counts as drift and prompts a recreate; until you accept, the kit's files and packages stay in the sandbox.

> **External kits (URL / OCI / ZIP):** the drift check hashes the reference string only, not the remote payload. If the contents at that URL change without the URL changing, sbxgo cannot detect it; bump the version in the reference (or run `sbxgo setup`) to force a recreate. Symlinks inside a local kit's `files/` are similarly skipped from the hash; commit the resolved files instead if their contents need to drive drift.

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
| `network_policy` | | `deny-all` | `allow-all`, `balanced`, or `deny-all`. Documentation only; sbxgo never changes the host-wide default. Set it with `sbx policy set-default`. |
| `clone` | | `false` | When true, pass `--clone` to sbx so the agent works in an in-container clone exposed back as the `sandbox-<name>` git remote (sbx 0.31.0+; replaces the removed `branch` field). |
| `required_secrets` | | | Secret names to check; missing ones warn, do not block |
| `allowed_domains` | | | Sandbox-scoped allow rules added on each run (sbx 0.29.0+). Re-add is idempotent. |
| `denied_domains` | | | Sandbox-scoped deny rules. Always wins over allow. |
| `kits` | | | Kit references applied at sandbox creation. Content changes count as drift and prompt a recreate on the next `sbxgo run`. |
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

> **Heads up:** `network_policy` is the *host-wide* baseline (`sbx policy
> set-default ...`). `sbxgo` doesn't change it for you; you'll see a
> warning if the configured value doesn't match the active default.
> `allowed_domains` / `denied_domains` are scoped to this sandbox in sbx
> 0.29.0+ and applied on every `sbxgo run`; re-adding the same rule is a
> no-op (sbx reports "Already covered").

### Secrets

Secret values never appear in config. `required_secrets` lists names only. `sbxgo run` and `sbxgo setup` check `sbx secret ls` and warn if any are missing. Set them once per machine:

```bash
sbx secret set MY_SECRET_TOKEN
```

---

## Contributing

Issues and PRs welcome.

PR titles must follow [Conventional Commits](https://www.conventionalcommits.org/) (e.g. `feat: add foo`, `fix: handle nil case`, `feat!: rename Run to Start` for breaking changes). The PR-title check enforces this. The repo is intentionally on `0.x`, so `feat!` bumps the minor version, not the major (see `.releaserc.yml`); when we're ready to graduate to 1.0.0 we'll remove that override.

Design goals:
- **Project-level**: config and Dockerfiles live in the repo, not on the developer's machine
- **Safe by default**: secrets never touch the filesystem or image layers
- **Testable**: all external calls go through interfaces; no global state

### Release process

Pushes to `main` trigger CI (lint + test) followed by `semantic-release`, which decides the next version from the conventional-commit history, creates the GitHub release with auto-generated notes, and then invokes `goreleaser` via `successCmd` to attach the binaries.

If `goreleaser` fails *after* `semantic-release` has already created the release (build flake, signing issue, transient network), the release will exist with notes but no binary assets. Recovery: re-run the release workflow on the same commit. `goreleaser`'s `release.mode: keep-existing` setting preserves the body and just appends the missing artifacts on the next attempt, with no need to delete and re-tag.

---

## License

MIT, see [LICENSE](./LICENSE). Third-party attributions are listed in [THIRD_PARTY_LICENSES.md](./THIRD_PARTY_LICENSES.md).
