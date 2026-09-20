// Command buses is the settings screen, with nothing on screen: two sources on
// two different Buses, and a slider moving one of them while the other does not
// budge. It is the case SetBus exists for.
//
//	go run ./cmd/sound/buses
//
// It needs no GPU and opens no window, and supplies its own app.MainLoop for
// the reason cmd/sound/orbit does: a game that only makes noise needs a ticker
// and not a renderer.
//
// What to listen for, and what to watch:
//
//   - Two notes sound together, one low and one high. The low one is "music"
//     and plays on Master; the high one is "effects" and plays on a Bus this
//     demo declared. Nothing about the two Voices differs except their Bus and
//     their pitch, so anything that moves one and not the other is the Bus
//     doing it.
//   - audibility is volume x bus, and the demo prints both sides of that
//     product: the Volume it commanded, the Bus volume sound holds as of the
//     last flush, and the audibility sound derived. The Bus volume is read back
//     off sound's own Buses view rather than remembered, which is exactly what
//     a settings screen does when it draws its slider where the player left it.
//
// # Master is the default Bus and not a global trim
//
// This is the one thing here that is not guessable, and it is why the script
// moves Master on its own before it moves anything else.
//
// Buses are flat: every Bus is directly under Master, and Master is Bus 0.
// "Directly under" is about StopBus, which stops everything when it names
// Master - and not about volume. A Bus's volume is folded into the Voices on
// that Bus and nowhere else, so turning Master down turns down the Voices that
// landed on Master, which is every Voice in a game that declared no Bus at all.
// It does not scale the Buses a game did declare.
//
// A settings screen with a master slider and two group sliders therefore writes
// three SetBus calls of its own arithmetic, and the last phase of the script
// below is what that looks like: one intent, every declared Bus moved. What
// sound owns is one volume per Bus; what a game owns is what its sliders mean.
//
// The demo ends on its own after the script runs out. Ctrl+C leaves early.
package main

import (
	"fmt"
	"os"
	"os/signal"
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

// clipOgg is the demo's one sound, embedded rather than vendored into assets/,
// for the reasons cmd/sound/orbit gives and ATTRIBUTION.md beside this file
// repeats. Both Voices play it; only the Bus and the pitch differ, which is
// what makes the Bus the only explanation for anything that moves.
//
//go:embed pianoroll.ogg
var clipOgg []byte

// Effects is the one Bus this demo declares. A game declares its own - music,
// effects, dialogue, UI - as constants of its own type; sound predeclares
// Master alone, and Master is zero, so a Voice that names no Bus lands there.
const Effects sound.Bus = 1

const (
	// musicVolume and effectsVolume are what the two Voices were told to play
	// at, and they never change: everything that moves below is a Bus volume,
	// so that the product the demo prints has one moving factor.
	musicVolume   float32 = 0.60
	effectsVolume float32 = 0.60
	// musicPitch and effectsPitch separate the two by ear, an octave down and a
	// fifth up off the same clip. Pitch is a Voice parameter and reaches the
	// Adapter as a rate; it is not part of the arithmetic this demo is about.
	musicPitch   float32 = 0.50
	effectsPitch float32 = 1.50
)

const (
	// tickRate is the demo's fixed update rate. app owns the accumulator; this
	// is only how often the ticker wakes the loop.
	tickRate = 60
	// printEvery is how many ticks pass between console lines. Twice a second
	// is enough: nothing here moves except at a script step, and the lines
	// between them are there to show that nothing drifts.
	printEvery = tickRate / 2
)

// step is one entry of the script: at this many seconds in, say this, and leave
// the two Bus volumes here until the next one.
type step struct {
	at      float64
	say     string
	master  float32
	effects float32
}

// script is the whole demo. Each step writes both Bus volumes outright rather
// than only the one that moved, because a settings screen restates what its
// sliders say and never accumulates deltas onto what sound already holds.
var script = []step{
	{at: 0, say: "both Buses at unity: the music on Master, the effects on Bus 1", master: 1.00, effects: 1.00},
	{at: 3, say: "the master slider moves - and only the Voice on Master follows it", master: 0.20, effects: 1.00},
	{at: 6, say: "master back to unity, the effects slider down instead", master: 1.00, effects: 0.20},
	{at: 9, say: "one global slider is the game's own arithmetic: every declared Bus moved", master: 0.20, effects: 0.20},
	{at: 12, say: "", master: 0, effects: 0}, // the end of the script; nothing is applied
}

func main() {
	cfg := map[kernel.PluginName]any{
		storage.Name: storage.Config{},
		sound.Name:   sound.Config{},
	}
	permanentfs.Configure(cfg)
	soundbackend.Configure(cfg)
	// Last of the contributors, so every plugin an override may name is in the
	// map by now.
	cfg = config.Inject(cfg)

	// storage is here because sound depends on it - a Clip named by a path is
	// read through it - even though this demo names its Clip by bytes.
	plugins := []kernel.Plugin{
		storageplugin.New(),
		permanentfs.New(),
		appplugin.New(),
		soundplugin.New(),
		soundbackend.New(), // sound's Backend Adapter for this platform
		newBuses(),
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
const Name kernel.PluginName = "buses"

// BusesMainLoop is the Adapter through which this demo fills app's MainLoop
// Port. A sound demo wants no window, and app's loop has to come from
// somewhere, so the demo is its own platform.
type BusesMainLoop kernel.Adapter[app.MainLoopPort]

// UpdateOnTick is the demo's one subscription: run the script, then read back
// what sound made of it.
type UpdateOnTick kernel.Subscription[app.UpdateEvent]

// buses is the demo plugin: the gameplay, the MainLoop Adapter and the Host, in
// one type, the way cmd/sound/orbit is.
type buses struct {
	loop  app.Loop
	quit  chan struct{}
	clip  sound.ClipRef
	music sound.Voice
	fx    sound.Voice

	elapsed float64
	ticks   int
	next    int // the script step to apply next
	done    bool
}

func newBuses() kernel.Plugin { return &buses{quit: make(chan struct{})} }

var (
	_ kernel.Plugin     = (*buses)(nil)
	_ kernel.PluginHost = (*buses)(nil)
	_ app.MainLoop      = (*buses)(nil)
)

func (b *buses) Name() kernel.PluginName { return Name }

// Dependencies names sound, whose resources this demo locks. app is not among
// them: subscribing to app.UpdateEvent needs no dependency, because an event
// nobody publishes is simply never delivered.
func (b *buses) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{sound.Name}
}

func (b *buses) Register(registrar *kernel.Registrar, _ any) error {
	b.clip = sound.ClipWithBytes(assets.NewBlob(clipOgg))
	registrar.ProvideAdapter[BusesMainLoop](app.MainLoop(b))
	registrar.Subscribe[UpdateOnTick](b.updateOnTick)
	return nil
}

func (b *buses) Start(kernel.Executioner) error { return nil }
func (b *buses) Stop(kernel.Executioner)        {}

// Attach keeps the Loop app hands over.
func (b *buses) Attach(loop app.Loop) { b.loop = loop }

// Quit asks Run to leave its loop. It may be called from a goroutine that is
// not the one inside Run and more than once, so the close is guarded.
func (b *buses) Quit() {
	select {
	case <-b.quit:
	default:
		close(b.quit)
	}
}

// ClipboardWrite refuses: there is no window and no clipboard to write to.
func (b *buses) ClipboardWrite(string) error {
	return fmt.Errorf("buses: no clipboard without a window")
}

// Run is the platform loop this demo stands in for: wake at the tick rate, hand
// app the real seconds that passed, and let its accumulator decide how many
// updates that is. Nothing is drawn, so Render is never called.
func (b *buses) Run(k kernel.Executioner) error {
	if err := b.loop.Init(k); err != nil {
		return err
	}
	defer b.loop.Quit(k)

	fmt.Println("buses: two Voices, two Buses, and a slider that moves one of them.")
	fmt.Println("       audibility is volume x bus; watch which side of the product moves.")
	fmt.Println()

	ticker := time.NewTicker(time.Second / tickRate)
	defer ticker.Stop()
	last := time.Now()
	for {
		select {
		case <-b.quit:
			return nil
		case now := <-ticker.C:
			dt := now.Sub(last).Seconds()
			last = now
			b.loop.Frame(k, dt)
			if b.done {
				return nil
			}
		}
	}
}

// updateOnTick starts the two Voices on the first tick, walks the script, and
// prints what sound made of it.
//
// Its lock set is a write on the queue and reads on the three views it reports.
// Where a Bus volume stands is sound's to hold, so asking is a read - which is
// the whole reason Buses is a resource a settings screen can lock on its own.
func (b *buses) updateOnTick() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var queue kernel.Write[*sound.Queue]
	var voices kernel.Read[*sound.Voices]
	var busVolumes kernel.Read[*sound.Buses]
	var device kernel.Read[*sound.Device]
	return func(access kernel.ResourceAccess) {
			queue = access.GetWrite[*sound.Queue]()
			voices = access.GetRead[*sound.Voices]()
			busVolumes = access.GetRead[*sound.Buses]()
			device = access.GetRead[*sound.Device]()
		}, func(_ kernel.Kernel, event app.UpdateEvent) {
			q := queue.Get()
			if b.music == sound.NoVoice {
				b.start(q)
			}
			b.elapsed += event.Dt
			b.advance(q)

			b.ticks++
			if b.ticks%printEvery == 0 {
				b.report(voices.Get(), busVolumes.Get(), device.Get())
			}
		}
}

// start plays both Voices. Neither is positional: a settings slider has nothing
// to do with where a sound is, and a Voice with no position is heard centred
// with no falloff, no cone and no panning - so audibility here is volume x bus
// and nothing else, which is what makes the product readable.
func (b *buses) start(q *sound.Queue) {
	b.music = q.Play(b.clip, 0, sound.Params{
		// No Bus named at all, which is what a game that declared none writes.
		// It lands on Master, and that is the point of this Voice.
		Volume: m.Some(musicVolume),
		Pitch:  m.Some(musicPitch),
		Loop:   m.Some(true),
	})
	b.fx = q.Play(b.clip, 0, sound.Params{
		Bus:    m.Some(Effects),
		Volume: m.Some(effectsVolume),
		Pitch:  m.Some(effectsPitch),
		Loop:   m.Some(true),
	})
}

// advance applies every script step whose moment has passed, and ends the demo
// when the script runs out.
func (b *buses) advance(q *sound.Queue) {
	for b.next < len(script) && b.elapsed >= script[b.next].at {
		at := script[b.next]
		b.next++
		if at.say == "" {
			b.done = true
			fmt.Println()
			fmt.Println("buses: the script is done.")
			return
		}
		fmt.Printf("\n%5.1fs  %s\n", at.at, at.say)
		q.SetBus(sound.Master, at.master)
		q.SetBus(Effects, at.effects)
	}
}

// report prints one line of what sound derived, for both Voices side by side.
// Every number but the commanded Volume is read off sound's own views: the Bus
// volumes off Buses, which is where a settings screen reads its slider
// positions from, and audibility off the live Voice.
//
// Both views answer as of the last flush, so a SetBus recorded in this tick
// shows up in the next one - which is true of every operation a game records,
// and is why the line after a script step still shows the old volume.
func (b *buses) report(live *sound.Voices, volumes *sound.Buses, dev *sound.Device) {
	fmt.Printf("%s | %s | %s\n",
		b.line("music  ", b.music, sound.Master, musicVolume, live, volumes),
		b.line("effects", b.fx, Effects, effectsVolume, live, volumes),
		deviceLine(dev))
}

// line is one Voice's half of the report: what the game commanded, what sound
// holds for its Bus, and what the two came to.
func (b *buses) line(
	label string, voice sound.Voice, bus sound.Bus, commanded float32,
	live *sound.Voices, volumes *sound.Buses,
) string {
	info, ok := live.Info(voice)
	if !ok {
		return fmt.Sprintf("%s bus %d  gone", label, bus)
	}
	return fmt.Sprintf("%s bus %d  vol %4.2f x bus %4.2f  audibility %4.2f",
		label, info.Bus, commanded, volumes.Volume(bus), info.Audibility)
}

// deviceLine says whether anything can be heard. A game reads Device rather
// than handling it: absent, not ready yet and lost all look the same from here,
// and every number above moves through all three.
func deviceLine(dev *sound.Device) string {
	if !dev.Ready {
		return "device: not ready (every number still moves)"
	}
	return fmt.Sprintf("device: %s %d Hz", dev.Name, dev.SampleRate)
}
