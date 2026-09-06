package main

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"time"
)

// source reads files out of the upstream repository at a pinned commit.
//
// Everything it fetches is cached on disk under the commit that produced it, so
// a second run does no network at all and two runs a year apart produce the
// same bytes. A cache entry can never go stale: its key contains the commit, and
// a commit's content does not change.
type source struct {
	cache  string
	client *http.Client
	loud   bool
}

func newSource(cache string, loud bool) *source {
	return &source{
		cache:  cache,
		client: &http.Client{Timeout: 5 * time.Minute},
		loud:   loud,
	}
}

// file returns the contents of one repository path at one commit.
func (s *source) file(commit, name string) ([]byte, error) {
	cached := filepath.Join(s.cache, commit, filepath.FromSlash(name))
	if data, err := os.ReadFile(cached); err == nil {
		return data, nil
	}

	url := fmt.Sprintf("%s/%s/%s", upstreamRaw, commit, name)
	if s.loud {
		fmt.Fprintf(os.Stderr, "  fetch %s\n", name)
	}
	response, err := s.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: %s", url, response.Status)
	}
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", url, err)
	}

	if err := os.MkdirAll(filepath.Dir(cached), 0o755); err != nil {
		return nil, fmt.Errorf("cache %s: %w", name, err)
	}
	if err := os.WriteFile(cached, data, 0o644); err != nil {
		return nil, fmt.Errorf("cache %s: %w", name, err)
	}
	return data, nil
}

// variantFS is one model variant directory, at one commit, seen as a
// filesystem. The glTF decoder resolves a document's external buffer URIs
// through it, so a loose .gltf variant loads from upstream exactly as it would
// from a checkout - which is what lets this tool treat the packed and the
// already-binary assets the same way.
type variantFS struct {
	source *source
	commit string
	prefix string
}

func (v variantFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	data, err := v.source.file(v.commit, path.Join(v.prefix, name))
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: err}
	}
	return &memoryFile{Reader: bytes.NewReader(data), name: path.Base(name), size: int64(len(data))}, nil
}

// ReadFile is what everything but the glTF decoder uses, and it is why
// variantFS exists as a named type rather than a closure.
func (v variantFS) ReadFile(name string) ([]byte, error) {
	return v.source.file(v.commit, path.Join(v.prefix, name))
}

type memoryFile struct {
	*bytes.Reader
	name string
	size int64
}

func (f *memoryFile) Close() error               { return nil }
func (f *memoryFile) Stat() (fs.FileInfo, error) { return memoryInfo{name: f.name, size: f.size}, nil }

type memoryInfo struct {
	name string
	size int64
}

func (i memoryInfo) Name() string       { return i.name }
func (i memoryInfo) Size() int64        { return i.size }
func (i memoryInfo) Mode() fs.FileMode  { return 0o444 }
func (i memoryInfo) ModTime() time.Time { return time.Time{} }
func (i memoryInfo) IsDir() bool        { return false }
func (i memoryInfo) Sys() any           { return nil }

var (
	_ fs.FS   = variantFS{}
	_ fs.File = (*memoryFile)(nil)
)
