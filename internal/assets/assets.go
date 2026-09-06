// Package assets mounts the vendored demo asset set through storage.
//
// It exists because storage.DefaultConfig on its own does not reach it. The
// default read mount is the executable's own directory, and `go run` builds
// into a temporary directory, so a demo that configures nothing finds no
// models - and finds them by silently loading zero of them, which is the worst
// available failure mode for a set of demos whose whole subject is that a model
// which fails to load is skipped rather than substituted.
//
//	config, err := assets.Config(storage.DefaultConfig("cog-examples"))
//
// Paths keep the assets/ prefix the repository uses, so a demo names a model
// exactly as ATTRIBUTION.md and the manifest do:
//
//	assets/Fox/Fox.glb
//	assets/broken/truncated.glb
//
// The mount is confined to the asset directory even so - the prefix is
// presented, not mounted - so nothing else in the checkout is readable through
// it.
//
// # Two mounts, one spelling
//
// Config is the whole of the platform difference, and a demo sees none of it.
// On disk it locates the checkout's assets/ directory and presents it under its
// own name. Under GOOS=js there is no filesystem to walk, so it takes the map
// that cmd/web/index.html unpacked out of assets.tar.gz before the module
// booted. Both land on the same mount id and answer the same assets/-prefixed
// paths, which is what lets every demo run in a browser without a line of its
// own about it.
package assets

import (
	"github.com/dvoyni/cog/storage"
)

const (
	// Mount is the storage mount id the asset set lands on, on every platform.
	Mount storage.MountId = "assets"
	// Dir is the directory the set lives in, and the prefix every asset path
	// carries - inside the repository, inside the tar, and inside the mount.
	Dir = "assets"
	// marker is the file that identifies a directory as this asset set rather
	// than some other directory called "assets". It is also what the web
	// bundle is checked for, since a tar missing it is a tar built from the
	// wrong directory.
	marker = "ATTRIBUTION.md"
)
