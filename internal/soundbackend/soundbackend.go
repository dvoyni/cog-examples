// Package soundbackend composes sound's Backend Adapter for the platform a
// demo is built for: otosound on the desktop, jssound in a browser.
//
// It exists for the same reason permanentfs does. sound declares BackendPort as
// a required Port and fills it with neither Adapter itself, and the two
// Adapters cannot both be compiled: otosound is !js because a Go PCM mixer on
// the browser's main thread starves, and jssound is js because it is Web Audio
// nodes. A demo that named either one directly would build on one platform
// only, and everything two levels under cmd/ is walked by the browser build.
//
// The asymmetry in the two Configs is deliberate and is the Adapters', not this
// package's: otosound asks for a buffer size because it owns the mixer, and
// jssound asks for a latency hint because the browser owns it. A demo that
// wants neither passes neither, which is what Configure does here.
package soundbackend
