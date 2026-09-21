// Package assets mounts the vendored demo asset set through storage.
//
// It exists because storage mounts nothing by default: a read mount is an fs.FS
// some plugin contributes through storage.ReadMountPort, and `go run` builds
// into a temporary directory, so a demo that contributes nothing finds no
// models - and finds them by silently loading zero of them, which is the worst
// available failure mode for a set of demos whose whole subject is that a model
// which fails to load is skipped rather than substituted.
//
// A demo that reads the set says so in its own plugin's Register, which is what
// keeps every composition of that plugin - main.go's, a test's - reading the
// same files:
//
//	mount, err := assets.Mount()
//	if err != nil {
//		return err
//	}
//	registrar.ProvideAdapter[assets.StorageReadMount](mount)
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
// Mount is the whole of the platform difference, and a demo sees none of it.
// On disk it locates the checkout's assets/ directory and presents it under its
// own name. Under GOOS=js there is no filesystem to walk, so it takes the map
// that cmd/web/index.html unpacked out of assets.tar.gz before the module
// booted. Both land on the same mount id and answer the same assets/-prefixed
// paths, which is what lets every demo run in a browser without a line of its
// own about it.
package assets

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/storage"
)

// StorageReadMount is the Adapter a demo's plugin contributes the asset set to
// storage as, providing the ReadMount that Mount returns. storage installs it
// at its own Start, ahead of every plugin that depends on storage, so a demo
// loading a model from its first frame finds it; a second contribution of the
// same mount fails that Start with storage.ErrDuplicateMount.
type StorageReadMount kernel.Adapter[storage.ReadMountPort]

const (
	// MountId is the storage mount id the asset set lands on, on every
	// platform. It is not called Mount only because the function that returns
	// the mount is.
	MountId storage.MountId = "assets"
	// Dir is the directory the set lives in, and the prefix every asset path
	// carries - inside the repository, inside the tar, and inside the mount.
	Dir = "assets"
	// marker is the file that identifies a directory as this asset set rather
	// than some other directory called "assets". It is also what the web
	// bundle is checked for, since a tar missing it is a tar built from the
	// wrong directory.
	marker = "ATTRIBUTION.md"
)
