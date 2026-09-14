//go:build js

package permanentfs

import (
	"github.com/dvoyni/cog/extensions/jsfs"
	"github.com/dvoyni/cog/extensions/jsfs/jsfsplugin"
	"github.com/dvoyni/cog/kernel"
)

// New returns the jsfs plugin. Its Config is the one Configure supplies.
func New() kernel.Plugin { return jsfsplugin.New() }

// Configure supplies jsfs's Config for AppId under jsfs.Name.
func Configure(config map[kernel.PluginName]any) { config[jsfs.Name] = jsfs.Config{AppId: AppId} }
