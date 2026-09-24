package main

import (
	"sync"
	"time"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// State is the demo's own state, shared by its Systems as a resource: the
// clock, the pick, the two composited textures and the numbers the HUD prints.
type State struct {
	step   int
	paused bool

	// picked indexes targets, and is -1 when the last click hit nothing. The
	// bool from pick is checked rather than the index compared, because an
	// index nobody checked would leave the last thing tinted for ever.
	picked  int
	targets []pickable

	// resident is whether the model's bounds have joined targets, which is the
	// first step model answers Bounds for the file.
	resident bool

	// spawned is how many Entities setup made, which the HUD prints: a count
	// the app knows because it made them.
	spawned int

	// The frame's two composited textures, published by the target System for
	// the HUD System to sample. They are frame-local handles and are rebuilt
	// every frame; holding one across a frame boundary would name a texture the
	// pool has already handed to something else.
	mainTexture gfx.TextureDescr
	mapTexture  gfx.TextureDescr

	// duplicate is held while D is down, and makes the duplicate System spawn
	// a second camera under CameraMain's id.
	duplicate bool

	// pointer is the last pointer position in canvas coordinates, kept so the
	// HUD can say which panel the cursor is over before anything is clicked.
	pointer m.Vec2

	// reports is what the engine reported, printed by the HUD: an error a demo
	// provokes on purpose has to be visible in the demo, or it is
	// indistinguishable from one it did not provoke.
	reports *reportLog

	rate rate
}

// newState is the demo at its documented starting pose, paused, with nothing
// picked.
func newState(reports *reportLog) *State {
	return &State{step: startStep, paused: true, picked: -1, reports: reports}
}

// time is the demo's clock: accumulated fixed steps, so step N is the same
// frame on every machine and a test drives N steps directly.
func (s *State) time() float32 { return float32(s.step) * fixedStep }

// pickedName is what the last click landed on, or the empty string.
func (s *State) pickedName() string {
	if s.picked < 0 || s.picked >= len(s.targets) {
		return ""
	}
	return s.targets[s.picked].name
}

// reportLog counts the engine's reports and keeps the latest. The error
// handler writes it from whichever goroutine reported, outside every System's
// locks, so it carries its own.
type reportLog struct {
	mu    sync.Mutex
	count int
	last  string
}

func (r *reportLog) add(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.count++
	r.last = err.Error()
}

func (r *reportLog) read() (int, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.count, r.last
}

// rate is the HUD's frames-per-second meter, and the demo's only wall clock. It
// counts frames over a window rather than averaging 1/interval per frame,
// because a per-frame average is dominated by its own worst sample: two ticks a
// microsecond apart during startup read as a million, and an exponential
// average carries a thousandth of that for a hundred frames afterwards.
type rate struct {
	window    time.Time
	frames    int
	perSecond float32
}

const ratePeriod = 250 * time.Millisecond

func (r *rate) measure(now time.Time) {
	if r.window.IsZero() {
		r.window = now
		return
	}
	r.frames++
	if elapsed := now.Sub(r.window); elapsed >= ratePeriod {
		r.perSecond = float32(float64(r.frames) / elapsed.Seconds())
		r.frames, r.window = 0, now
	}
}
