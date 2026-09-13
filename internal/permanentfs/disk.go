//go:build !js

package permanentfs

import (
	"github.com/dvoyni/cog/extensions/diskfs"
	"github.com/dvoyni/cog/kernel"
)

// New returns the diskfs plugin for AppId.
func New() kernel.Plugin { return diskfs.New(diskfs.Config{AppId: AppId}) }
