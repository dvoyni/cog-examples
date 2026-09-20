//go:build !js

package soundbackend

import (
	"github.com/dvoyni/cog/extensions/otosound"
	"github.com/dvoyni/cog/extensions/otosound/otosoundplugin"
	"github.com/dvoyni/cog/kernel"
)

// New returns the otosound plugin: our own mixer over ebitengine/oto, on the
// device thread the Adapter owns.
func New() kernel.Plugin { return otosoundplugin.New() }

// Configure supplies otosound's Config under otosound.Name. Every field is left
// at its zero value on purpose: 0 means a 10 ms buffer and 48000 Hz, and a demo
// that named either would be stating a number it has no reason to have an
// opinion about.
func Configure(config map[kernel.PluginName]any) {
	config[otosound.Name] = otosound.Config{}
}
