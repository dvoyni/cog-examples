//go:build !js

package permanentfs

import (
	"github.com/dvoyni/cog/extensions/diskfs"
	"github.com/dvoyni/cog/extensions/diskfs/diskfsplugin"
	"github.com/dvoyni/cog/kernel"
)

// New returns the diskfs plugin. Its Config is the one Configure supplies.
func New() kernel.Plugin { return diskfsplugin.New() }

// Configure supplies diskfs's Config for AppId under diskfs.Name.
func Configure(config map[kernel.PluginName]any) { config[diskfs.Name] = diskfs.Config{AppId: AppId} }
