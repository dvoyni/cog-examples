// Package permanentfs composes storage's PermanentFS Adapter for the platform a
// demo is built for: diskfs on the desktop, jsfs in a browser.
//
// storage requires exactly one Adapter, and the two cog ships are each built
// only for their own platform, so a demo that named either would stop
// cross-compiling for the other. Every demo composes this instead, which keeps
// the browser build free of any line of its own, as internal/assets does for
// the asset mount. New is the plugin, and Configure adds its Config to the
// engine's config map:
//
//	config := map[kernel.PluginName]any{storage.Name: storage.Config{}, …}
//	permanentfs.Configure(config)
//	plugins := []kernel.Plugin{storageplugin.New(), permanentfs.New(), …}
package permanentfs

// AppId is the application id every demo saves under: a directory under the
// user's data directory on the desktop, and part of the localStorage key in a
// browser.
const AppId = "cog-examples"
