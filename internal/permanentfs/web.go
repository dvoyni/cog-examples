//go:build js

package permanentfs

import (
	"github.com/dvoyni/cog/extensions/jsstorage"
	"github.com/dvoyni/cog/extensions/jsstorage/jsstorageplugin"
	"github.com/dvoyni/cog/kernel"
)

// New returns the jsstorage plugin. Its Config is the one Configure supplies.
func New() kernel.Plugin { return jsstorageplugin.New() }

// Configure supplies jsstorage's Config for AppId under jsstorage.Name.
func Configure(config map[kernel.PluginName]any) {
	config[jsstorage.Name] = jsstorage.Config{AppId: AppId}
}
