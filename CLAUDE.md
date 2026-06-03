# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with this repository.

## What this is

`sbxgo` is a single Go binary for running AI coding agents in [Docker Sandboxes](https://github.com/docker/sbx-releases) with two subcommands:

- **`sbxgo setup`**: builds a Docker template image from a project Dockerfile and creates the sandbox. Run once when setting up a project or when the Dockerfile changes.
- **`sbxgo run`**: everyday runner; reads `.sbxgo/config.toml`, applies network policy, checks secrets, and resumes or creates the sandbox.

`install.sh` downloads the pre-built binary from GitHub Releases.

## Build and test

```bash
go build ./...
go test ./...
golangci-lint run ./...
go install ./cmd/sbxgo
```

Run a single test:

```bash
go test ./internal/config/... -run TestLoadConfig_Valid
```

Use `--dry-run` to verify behavior without executing:

```bash
sbxgo setup --dry-run
sbxgo run --dry-run
```

## Architecture

Single entry point in `cmd/sbxgo` is a cobra root command with two subcommands (`setup` and `run`) that delegate to `internal/sandbox.Setup()` and `internal/sandbox.Start()` respectively. The CLI command `run` maps to the internal `Start` function; they are synonymous (start/resume the sandbox).

All external calls go through interfaces injected at the top level:
- `runner.CommandRunner`: wraps `exec.Cmd`; `runner.NewFakeRunner()` for tests
- `fsutil.FileSystem`: wraps file I/O; `fsutil.NewFakeFileSystem()` for tests
- `prompt.Prompter`: wraps stdin confirmation; `prompt.NewFakePrompter()` for tests

Key internal packages:
- `internal/config`: loads and validates `.sbxgo/config.toml`; `config.Load` uses the real OS, `config.Parse` takes bytes (used in tests via the fake filesystem)
- `internal/sandbox`: orchestration logic (`setup.go`, `start.go`, `policy.go`, `common.go`, `scaffold.go`)
- `internal/sbx`: thin client over the `sbx` CLI
- `internal/docker`: thin client over the `docker` CLI

## Error handling

All errors use `github.com/rotisserie/eris`. Use `eris.New`/`eris.Errorf` for new errors, `eris.Wrap`/`eris.Wrapf` to wrap. At the top-level entry points (cmd packages), print errors with `eris.ToString(err, true)` to include the stack trace.

## External references

- **Minimum sbx version**: declared as `sbx.MinVersion` (`internal/sbx/version.go`) and enforced by `Client.CheckMinVersion`, wired into the root cobra command's `PersistentPreRunE`. Bump both the constant and the README "Requires" note when raising the floor.
- **Releases & issue tracker**: https://github.com/docker/sbx-releases, check here for new sbx versions and to see if a problem you hit has already been reported.
- **Docs**: https://docs.docker.com/ai/sandboxes/, official documentation for Docker Sandboxes (the `sbx` CLI).
- **Always verify sbx CLI usage**: before adding or changing how this project invokes `sbx`, run `sbx <command> --help` and confirm flag names, positions, and semantics. Do not rely on memory, since the CLI evolves between releases.

## .sbxgo/config.toml field reference

| Field | Required | Default | Description |
|---|---|---|---|
| `agent` | yes | | `claude`, `codex`, `kiro`, `shell`, etc. |
| `[docker]` | | | Source of the template image. Set exactly one of `image` or `[docker.build]` (or omit the section to use sbx's default base). |
| `docker.image` | | | Registry reference, e.g. `ghcr.io/acme/dev:1.4.0`. Pulled by `sbxgo setup`. |
| `docker.build.context` | | `.` | Build context passed to `docker build`. |
| `docker.build.dockerfile` | | `.sbxgo/Dockerfile` | Path to the Dockerfile. |
| `network_policy` | | `deny-all` | `allow-all`, `balanced`, or `deny-all`. Documentation only; sbxgo never changes the host-wide default. Set it with `sbx policy set-default`. |
| `clone` | | `false` | When true, pass `--clone` to sbx (sbx 0.31.0+; replaces the removed `branch` field). |
| `required_secrets` | | | Names to check; missing ones warn, do not block |
| `allowed_domains` | | | Sandbox-scoped allow rules added each run (sbx 0.29.0+). Re-add is idempotent. |
| `denied_domains` | | | Sandbox-scoped deny rules. Always wins over allow. |
| `kits` | | | Kit references applied at `sbx create` only. Content changes are tracked by the drift hash and prompt a recreate on the next `sbxgo run`. |
| `extra_workspaces` | | | Extra host paths to mount into the sandbox |
