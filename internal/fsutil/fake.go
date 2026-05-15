package fsutil

import (
	"io/fs"
	"sort"
	"strings"

	"github.com/rotisserie/eris"
)

// FakeFileSystem is a test fake for FileSystem that stores files in memory.
type FakeFileSystem struct {
	// Files maps path [setup] content.
	Files map[string][]byte
	// Dirs records paths passed to MkdirAll.
	Dirs []string
	// CopyDirCalls records (src, dst) pairs passed to CopyDir.
	CopyDirCalls []CopyDirCall
	// WriteError is returned by WriteFile (if non-nil).
	WriteError error
	// ReadError is returned by ReadFile (if non-nil).
	ReadError error
}

// CopyDirCall records a single call to CopyDir.
type CopyDirCall struct {
	Src string
	Dst string
}

// NewFakeFileSystem creates a FakeFileSystem with an empty file store.
func NewFakeFileSystem() *FakeFileSystem {
	return &FakeFileSystem{
		Files: make(map[string][]byte),
	}
}

// ReadFile returns the stored content or an error if the path is not found.
func (f *FakeFileSystem) ReadFile(path string) ([]byte, error) {
	if f.ReadError != nil {
		return nil, f.ReadError
	}

	data, ok := f.Files[path]
	if !ok {
		return nil, eris.Errorf("fake: file not found: %q", path)
	}

	return data, nil
}

// WriteFile stores data in the in-memory map.
func (f *FakeFileSystem) WriteFile(path string, data []byte, _ fs.FileMode) error {
	if f.WriteError != nil {
		return f.WriteError
	}

	f.Files[path] = data

	return nil
}

// Exists reports whether a file exists in the in-memory map.
func (f *FakeFileSystem) Exists(path string) (bool, error) {
	_, ok := f.Files[path]
	return ok, nil
}

// MkdirAll records the call.
func (f *FakeFileSystem) MkdirAll(path string, _ fs.FileMode) error {
	f.Dirs = append(f.Dirs, path)
	return nil
}

// CopyDir records the call.
func (f *FakeFileSystem) CopyDir(src, dst string) error {
	f.CopyDirCalls = append(f.CopyDirCalls, CopyDirCall{Src: src, Dst: dst})
	return nil
}

// WalkFiles returns all in-memory paths that begin with root+"/" or equal
// root, sorted, with the prefix stripped. Used by the kit content hasher in
// tests to deterministically reproduce a directory walk.
func (f *FakeFileSystem) WalkFiles(root string) ([]string, error) {
	prefix := strings.TrimRight(root, "/") + "/"

	var files []string

	for path := range f.Files {
		if rel, ok := strings.CutPrefix(path, prefix); ok {
			files = append(files, rel)
		}
	}

	sort.Strings(files)

	return files, nil
}
