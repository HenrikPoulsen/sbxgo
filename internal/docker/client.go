// Package docker provides thin wrappers around the docker CLI.
package docker

import (
	"context"
	"strings"

	"github.com/rotisserie/eris"

	"github.com/HenrikPoulsen/sbxgo/internal/runner"
)

// Client wraps the docker CLI.
type Client struct {
	runner runner.CommandRunner
}

// NewClient creates a new docker Client.
func NewClient(r runner.CommandRunner) *Client {
	return &Client{runner: r}
}

// Build runs `docker build --iidfile <iidFile> -t <tag> -f <dockerfile> <context>`.
func (c *Client) Build(ctx context.Context, iidFile, tag, dockerfile, buildContext string) error {
	if buildContext == "" {
		buildContext = "."
	}

	err := c.runner.Run(ctx, "docker", "build",
		"--iidfile", iidFile,
		"-t", tag,
		"-f", dockerfile,
		buildContext,
	)
	if err != nil {
		return eris.Wrapf(err, "docker build (tag=%q, dockerfile=%q, context=%q)", tag, dockerfile, buildContext)
	}

	return nil
}

// Pull runs `docker pull <ref>`.
func (c *Client) Pull(ctx context.Context, ref string) error {
	if err := c.runner.Run(ctx, "docker", "pull", ref); err != nil {
		return eris.Wrapf(err, "docker pull %q", ref)
	}

	return nil
}

// InspectID returns the local image ID for a tag or reference, via
// `docker image inspect --format '{{.Id}}' <ref>`.
func (c *Client) InspectID(ctx context.Context, ref string) (string, error) {
	out, err := c.runner.Output(ctx, "docker", "image", "inspect", "--format", "{{.Id}}", ref)
	if err != nil {
		return "", eris.Wrapf(err, "docker image inspect %q", ref)
	}

	return strings.TrimSpace(string(out)), nil
}

// Save runs `docker image save -o <outputPath> <tag>`.
func (c *Client) Save(ctx context.Context, tag, outputPath string) error {
	if err := c.runner.Run(ctx, "docker", "image", "save", "-o", outputPath, tag); err != nil {
		return eris.Wrapf(err, "docker image save %q to %q", tag, outputPath)
	}

	return nil
}

// Tag runs `docker tag <src> <dst>`.
func (c *Client) Tag(ctx context.Context, src, dst string) error {
	if err := c.runner.Run(ctx, "docker", "tag", src, dst); err != nil {
		return eris.Wrapf(err, "docker tag %q as %q", src, dst)
	}

	return nil
}
