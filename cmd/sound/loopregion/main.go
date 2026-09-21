// Command loopregion plays a clip that says where it loops: a second of intro
// heard once, then four seconds of a ragtime waltz that repeat with no gap, and
// the playhead printed so the wrap can be seen as well as heard.
//
//	go run ./cmd/sound/loopregion
//
// It needs no GPU and opens no window, and supplies its own app.MainLoop for
// the reason cmd/sound/orbit does.
//
// # What it is evidence for
//
// A loop point is a fact about a Clip, not about a play. The clip here carries
// LOOPSTART and LOOPLENGTH in its own Vorbis comments, and the demo's one Play
// says nothing but Loop: true. Where that loop goes is the file's to say: the
// Adapter reads the region in the same header pass that reads the duration, and
// sound wraps the playhead at the region's end rather than the Clip's.
//
// So there are three things to watch:
//
//   - The first wrap goes back to 1.00 s, not to zero. The intro before it is
//     heard once and never again, and the summary says so from the lowest
//     playhead seen after that wrap, not from the demo's own expectations.
//   - The wrap happens at 5.04 s, although the Clip is 5.21 s long. The tail
//     past the loop end is in the file and is never heard, which is what tells
//     a Loop Region from "repeat the whole Clip".
//   - There is no gap. The playhead on the tick of a wrap is the loop start
//     plus whatever part of that tick ran past the loop end, never the loop
//     start plus a pause. And the loop end was chosen where the recording lines
//     up with its own loop start - first where the spectrum four seconds on best
//     matches the spectrum there, then at the sample where the two waveforms
//     meet - so the wrap should be hard to hear at all.
//
// Every number comes off sound's live Voice view. The clip is 5.2 s of stereo
// 44.1 kHz, 1.8 MiB decoded, so it is over otosound's 512 KiB resident limit
// and streams: the wrap is a seek of the Adapter's decoder, and sound's own
// playhead wraps with it whether or not a device is open.
//
// The clip was made once by cmd/looptag and committed; see ATTRIBUTION.md beside
// this file. clip_test.go checks that the tags on it are the region this file
// draws, and that it is the tool's output over the recording it came from.
//
// The demo ends on its own after the loop has come round three times. Ctrl+C
// leaves early.
package main

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	_ "embed"

	"github.com/dvoyni/cog-examples/internal/permanentfs"
	"github.com/dvoyni/cog-examples/internal/soundbackend"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/assets"
	"github.com/dvoyni/cog/libs/config"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
	"github.com/dvoyni/cog/slots/app/appplugin"
	"github.com/dvoyni/cog/slots/sound"
	"github.com/dvoyni/cog/slots/sound/soundplugin"
	"github.com/dvoyni/cog/slots/storage"
	"github.com/dvoyni/cog/slots/storage/storageplugin"
)

// clipOgg is the tagged clip, embedded for the reason orbit embeds its own.
//
//go:embed pianoroll-loop.ogg
var clipOgg []byte

// The Loop Region the clip declares, in its own sample frames. sound never
// shows a game a Clip's region, so these are what the demo draws its bar with -
// and clip_test.go reads the clip's tags back and fails if they ever say
// anything else, so the bar cannot drift from the file.
const (
	clipRate   = 44100
	loopStart  = 43960  // 0.9968 s: the intro
	loopLength = 178383 // 4.0450 s: the loop
)

var (
	loopFrom = float64(loopStart) / clipRate
	loopTo   = float64(loopStart+loopLength) / clipRate
)

const (
	// tickRate is the demo's fixed update rate. The wrap is looked for on
	// every tick; the playhead is printed on every printEvery-th.
	tickRate   = 60
	printEvery = tickRate / 8
	// laps is how many times the loop comes round before the demo stops.
	laps = 3
	// linger is how long the demo keeps playing after the last wrap, so the
	// line after it shows the loop carrying on.
	linger = 0.5
)

func main() {
	cfg := map[kernel.PluginName]any{
		storage.Name: storage.Config{},
		sound.Name:   sound.Config{},
	}
	permanentfs.Configure(cfg)
	soundbackend.Configure(cfg)
	cfg = config.Inject(cfg)

	// storage is here because sound depends on it, even though this demo names
	// its Clip by bytes.
	plugins := []kernel.Plugin{
		storageplugin.New(),
		permanentfs.New(),
		appplugin.New(),
		soundplugin.New(),
		soundbackend.New(), // sound's Backend Adapter for this platform
		newLoopRegion(),
	}

	engine := kernel.New(cfg).WithPlugins(plugins...)
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	go func() {
		<-interrupt
		engine.Quit()
	}()
	if err := engine.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// Name is the demo plugin's name.
const Name kernel.PluginName = "loopregion"

// LoopRegionMainLoop is the Adapter through which this demo fills app's
// MainLoop Port. A sound demo wants no window, so the demo is its own platform.
type LoopRegionMainLoop kernel.Adapter[app.MainLoopPort]

// UpdateOnTick plays the clip, watches its playhead and prints it.
type UpdateOnTick kernel.Subscription[app.UpdateEvent]

// wrap is one time the playhead went backwards: where it was on the tick
// before, and where it was on the tick of.
type wrap struct{ from, to float32 }

// loopRegion is the demo plugin: the gameplay, the MainLoop Adapter and the
// Host, in one type, the way cmd/sound/orbit is.
type loopRegion struct {
	loop  app.Loop
	quit  chan struct{}
	clip  sound.ClipRef
	voice sound.Voice

	ticks    int
	last     float32 // the playhead on the tick before, once the Voice is bound
	bound    bool
	duration float32
	wraps    []wrap
	lowest   float32 // the lowest playhead seen since the first wrap
	after    float64 // seconds played since the last wrap
	done     bool
}

func newLoopRegion() kernel.Plugin { return &loopRegion{quit: make(chan struct{})} }

var (
	_ kernel.Plugin     = (*loopRegion)(nil)
	_ kernel.PluginHost = (*loopRegion)(nil)
	_ app.MainLoop      = (*loopRegion)(nil)
)

func (l *loopRegion) Name() kernel.PluginName { return Name }

// Dependencies names sound, whose resources this demo locks.
func (l *loopRegion) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{sound.Name}
}

func (l *loopRegion) Register(registrar *kernel.Registrar, _ any) error {
	l.clip = sound.ClipWithBytes(assets.NewBlob(clipOgg))
	registrar.ProvideAdapter[LoopRegionMainLoop](app.MainLoop(l))
	registrar.Subscribe[UpdateOnTick](l.updateOnTick)
	return nil
}

func (l *loopRegion) Start(kernel.Executioner) error { return nil }
func (l *loopRegion) Stop(kernel.Executioner)        {}

// Attach keeps the Loop app hands over.
func (l *loopRegion) Attach(loop app.Loop) { l.loop = loop }

// Quit asks Run to leave its loop, guarded because it may arrive twice and from
// another goroutine.
func (l *loopRegion) Quit() {
	select {
	case <-l.quit:
	default:
		close(l.quit)
	}
}

// ClipboardWrite refuses: there is no window and no clipboard to write to.
func (l *loopRegion) ClipboardWrite(string) error {
	return fmt.Errorf("loopregion: no clipboard without a window")
}

// Run is the platform loop this demo stands in for.
func (l *loopRegion) Run(k kernel.Executioner) error {
	if err := l.loop.Init(k); err != nil {
		return err
	}
	defer l.loop.Quit(k)

	fmt.Printf("loopregion: a clip whose own tags say it loops over [%.2fs, %.2fs).\n", loopFrom, loopTo)
	fmt.Println("            the Play says only Loop: true. listen for the intro once, then no seam.")
	fmt.Println("            . intro   = loop   _ past the loop end, never heard   # playhead")
	fmt.Println()

	ticker := time.NewTicker(time.Second / tickRate)
	defer ticker.Stop()
	last := time.Now()
	for {
		select {
		case <-l.quit:
			return nil
		case now := <-ticker.C:
			dt := now.Sub(last).Seconds()
			last = now
			// Frame waits on every UpdateEvent it publishes, so done is
			// written and read either side of that edge rather than raced on.
			l.loop.Frame(k, dt)
			if l.done {
				return nil
			}
		}
	}
}

// updateOnTick plays the clip on the first tick, then reads its playhead on
// every tick: a playhead lower than the tick before is a wrap, and it is caught
// on the tick it happens rather than on the next printed line.
//
// Its lock set is a write on the queue and reads on the two views it reports.
func (l *loopRegion) updateOnTick() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var queue kernel.Write[*sound.Queue]
	var voices kernel.Read[*sound.Voices]
	var device kernel.Read[*sound.Device]
	return func(access kernel.ResourceAccess) {
			queue = access.GetWrite[*sound.Queue]()
			voices = access.GetRead[*sound.Voices]()
			device = access.GetRead[*sound.Device]()
		}, func(_ kernel.Kernel, event app.UpdateEvent) {
			q := queue.Get()
			if l.voice == sound.NoVoice {
				// Non-positional, and nothing about the loop: the region is the
				// Clip's, so Loop: true is the whole of what a game says.
				l.voice = q.Play(l.clip, 0, sound.Params{
					Volume: m.Some[float32](0.8),
					Loop:   m.Some(true),
				})
				return
			}
			live, dev := voices.Get(), device.Get()
			info, ok := live.Info(l.voice)
			if !ok || info.Duration == 0 {
				// Not in the view yet, or waiting on its Clip: nothing to watch.
				if l.ticks++; l.ticks%printEvery == 0 {
					fmt.Printf("waiting on the clip   %s\n", deviceLine(dev))
				}
				return
			}
			l.duration = info.Duration
			l.watch(info.Playhead, event.Dt)
			if l.ticks++; l.ticks%printEvery == 0 {
				fmt.Printf("%s  head %5.2fs  %d/%d voices   %s\n",
					l.bar(info.Playhead), info.Playhead, live.Len(), live.Cap(), deviceLine(dev))
			}
			if len(l.wraps) >= laps && l.after >= linger {
				q.Stop(l.voice)
				l.summary()
				l.done = true
			}
		}
}

// watch looks for the wrap: the one thing a looping playhead does that nothing
// else does is go backwards.
func (l *loopRegion) watch(head float32, dt float64) {
	if !l.bound {
		l.bound, l.last = true, head
		return
	}
	if len(l.wraps) > 0 {
		l.lowest = min(l.lowest, head)
		l.after += dt
	}
	if head < l.last {
		w := wrap{from: l.last, to: head}
		if len(l.wraps) == 0 {
			l.lowest = head
		}
		l.wraps = append(l.wraps, w)
		l.after = 0
		fmt.Printf("        -> wrap %d: the playhead went from %.3fs back to %.3fs, not to 0\n",
			len(l.wraps), w.from, w.to)
	}
	l.last = head
}

// bar draws the whole Clip, with the region the file declares marked on it and
// the playhead where sound says it is.
func (l *loopRegion) bar(head float32) string {
	const width = 52
	var b strings.Builder
	at := int(float64(head) / float64(l.duration) * width)
	for i := range width {
		t := (float64(i) + 0.5) / width * float64(l.duration)
		switch {
		case i == at:
			b.WriteByte('#')
		case t < loopFrom:
			b.WriteByte('.')
		case t < loopTo:
			b.WriteByte('=')
		default:
			b.WriteByte('_')
		}
	}
	return "|" + b.String() + "|"
}

// summary is what the playhead said, taken from the wraps it was seen making.
func (l *loopRegion) summary() {
	fmt.Println()
	fmt.Printf("loopregion: %d wraps, every one from the loop end back to the loop start:\n", len(l.wraps))
	for i, w := range l.wraps {
		fmt.Printf("            %d. %.3fs -> %.3fs\n", i+1, w.from, w.to)
	}
	fmt.Printf("            after the first wrap the playhead never went below %.3fs, so the %.2fs intro played once;\n",
		l.lowest, loopFrom)
	fmt.Printf("            the Clip is %.3fs long and the %.3fs past the loop end was never reached.\n",
		l.duration, float64(l.duration)-loopTo)
}

// deviceLine says whether anything can be heard. A game reads Device rather
// than handling it, and every number above moves without one.
func deviceLine(dev *sound.Device) string {
	if !dev.Ready {
		return "device: not ready (the playhead still moves)"
	}
	return fmt.Sprintf("device: %s %d Hz", dev.Name, dev.SampleRate)
}
