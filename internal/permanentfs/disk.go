//go:build !js

package permanentfs

import (
	"github.com/dvoyni/cog/extensions/diskstorage"
	"github.com/dvoyni/cog/extensions/diskstorage/diskstorageplugin"
	"github.com/dvoyni/cog/kernel"
)

// New returns the diskstorage plugin. Its Config is the one Configure supplies.
func New() kernel.Plugin { return diskstorageplugin.New() }

// Configure supplies diskstorage's Config for AppId under diskstorage.Name.
func Configure(config map[kernel.PluginName]any) {
	config[diskstorage.Name] = diskstorage.Config{AppId: AppId}
}
