//go:build js

package soundbackend

import (
	"github.com/dvoyni/cog/extensions/jssound"
	"github.com/dvoyni/cog/extensions/jssound/jssoundplugin"
	"github.com/dvoyni/cog/kernel"
)

// New returns the jssound plugin: Web Audio nodes carrying sound's own gain
// matrix, on the audio thread the browser owns.
func New() kernel.Plugin { return jssoundplugin.New() }

// Configure supplies jssound's Config under jssound.Name, left at its zero
// value: 0 means the browser's interactive latency hint, and the decoded-clip
// limit's 0 means its 512 KiB default.
//
// A page makes no sound until a gesture resumes its AudioContext, which is why
// a demo reads sound.Device.Ready rather than assuming it can be heard. jssound
// installs the listener that resumes it, so a demo does nothing about it beyond
// saying so on screen.
func Configure(config map[kernel.PluginName]any) {
	config[jssound.Name] = jssound.Config{}
}
