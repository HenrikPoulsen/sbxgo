// Package fsutil provides the FileSystem interface and its real implementation.
package fsutil

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/rotisserie/eris"
)

// FileSystem is the interface for filesystem operations.
type FileSystem interface {
	ReadFile(path string) ([]byte, error)
	WriteFile(path string, data []byte, perm fs.FileMode) error
	Exists(path string) (bool, error)
	MkdirAll(path string, perm fs.FileMode) error
	CopyDir(src, dst string) error
	// WalkFiles returns sorted forward-slash-separated paths of every
	// regular file under root, relative to root. Symlinks and directories
	// are not included. Returns an empty slice (and no error) if root does
	// not exist; callers can treat "no files" and "no directory" the same.
	WalkFiles(root string) ([]string, error)
}

// Real is the real FileSystem implementation using the OS.
type Real struct{}

// NewReal returns a Real FileSystem.
func NewReal() *Real {
	return &Real{}
}

// ReadFile reads all bytes from the given path.
func (r *Real) ReadFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path) //nolint:gosec
	if err != nil {
		return nil, eris.Wrapf(err, "reading file %q", path)
	}

	return data, nil
}

// WriteFile writes data to a file at the given path.
func (r *Real) WriteFile(path string, data []byte, perm fs.FileMode) error {
	if err := os.WriteFile(path, data, perm); err != nil {
		return eris.Wrapf(err, "writing file %q", path)
	}

	return nil
}

// Exists reports whether a path exists on the filesystem.
func (r *Real) Exists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}

	if os.IsNotExist(err) {
		return false, nil
	}

	return false, eris.Wrapf(err, "checking existence of %q", path)
}

// MkdirAll creates the named directory and any missing parents.
func (r *Real) MkdirAll(path string, perm fs.FileMode) error {
	if err := os.MkdirAll(path, perm); err != nil {
		return eris.Wrapf(err, "creating directory %q", path)
	}

	return nil
}

// CopyDir copies the contents of src into dst using os.CopyFS (Go 1.23+).
func (r *Real) CopyDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil { //nolint:gosec
		return eris.Wrapf(err, "creating destination %q", dst)
	}

	if err := os.CopyFS(dst, os.DirFS(src)); err != nil {
		return eris.Wrapf(err, "copying %q to %q", src, dst)
	}

	return nil
}

// WalkFiles walks root and returns relative paths of every regular file
// found under it, sorted lexicographically with forward-slash separators.
func (r *Real) WalkFiles(root string) ([]string, error) {
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}

		return nil, eris.Wrapf(err, "stat %q", root)
	}

	if !info.IsDir() {
		return nil, nil
	}

	var files []string

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !d.Type().IsRegular() {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return eris.Wrapf(err, "computing relative path for %q under %q", path, root)
		}

		files = append(files, filepath.ToSlash(rel))

		return nil
	})
	if walkErr != nil {
		return nil, eris.Wrapf(walkErr, "walking %q", root)
	}

	sort.Strings(files)

	return files, nil
}
