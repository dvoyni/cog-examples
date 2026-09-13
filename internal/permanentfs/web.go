//go:build js

package permanentfs

import (
	"github.com/dvoyni/cog/extensions/jsfs"
	"github.com/dvoyni/cog/kernel"
)

// New returns the jsfs plugin for AppId.
func New() kernel.Plugin { return jsfs.New(jsfs.Config{AppId: AppId}) }
