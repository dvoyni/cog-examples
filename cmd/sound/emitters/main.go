// Command emitters is the ecsaudio binding with nothing else composed around
// it: three Entities, one of which is despawned mid-play, and one of which
// finishes its one-shot and is deliberately left sitting there.
//
//	go run ./cmd/sound/emitters
//
// It needs no GPU and opens no window, and supplies its own app.MainLoop for
// the reason cmd/sound/orbit does. There is no renderer here at all, which is
// the point: ecsaudio is a binding from ecs to sound and touches neither scene
// nor gfx, so a demo of it draws nothing.
//
// # The two rules it is evidence for
//
// **A Voice dies with its Entity.** The siren is despawned six seconds in,
// while it is still looping. Nothing stops it and nothing tells sound about it;
// the Entity stops matching the binding's query, the binding notices on its
// next reconcile, and the Voice ends with ReasonStopped in that tick's flush.
//
// **A finished one-shot does not restart.** The chime plays once, through the
// 1.10 s of Clip - which at its rate of 1.6 is 0.69 s of wall clock, because a
// Voice's rate scales its playhead - and then its Entity goes on carrying an
// Emitter with nothing playing, for three seconds, in full view of a binding
// that reconciles every tick. That is the one rule of the package worth learning,
// and it follows from the table entry outliving the Voice it names: were the
// rule "an Emitter with no live Voice gets a Play", every one-shot in the game
// would restart forever, because a finished Voice is exactly a Voice that is no
// longer live.
//
// Re-triggering it is remove-then-re-add, which the script does next, a tick
// apart - the removal and the addition have to fall in different ticks, because
// the binding reconciles once per tick and an Emitter that left and came back
// inside one has not changed at all.
//
// # What is printed
//
// Two censuses beside each other: the Entities, out of ecs, and the Voices, out
// of sound's live view. Nothing in the Voice census is the demo's own
// bookkeeping - the demo never sees a sound.Voice handle at all, because the
// binding owns the correspondence and hands out nothing. The two Voices are
// told apart by the rate each was commanded at, which is the one thing about
// them the demo does know.
//
// The demo ends on its own after the script runs out. Ctrl+C leaves early.
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
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecs/ecsplugin"
	"github.com/dvoyni/cog/bundles/ecsaudio"
	"github.com/dvoyni/cog/bundles/ecsaudio/ecsaudioplugin"
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

// clipOgg is the demo's one sound, embedded rather than vendored into assets/.
// See ATTRIBUTION.md beside this file. Both Emitters name it, so the difference
// between them is the Params and the Transform and nothing else.
//
//go:embed pianoroll.ogg
var clipOgg []byte

const (
	// sirenPitch and chimePitch are how the two Voices are told apart in the
	// census. The demo holds no Voice handle - the binding does - so the rate
	// it commanded is what it recognises its own sounds by.
	sirenPitch float32 = 0.60
	chimePitch float32 = 1.60

	// sirenRange is how far the siren stands from the listener, and the Ref of
	// its Falloff, so the circle it sits on is unity and distance is not
	// quietly changing the audibility this demo is reading.
	sirenRange = 3
)

const (
	// tickRate is the demo's fixed update rate.
	tickRate = 60
	// printEvery is how many ticks pass between census lines.
	printEvery = tickRate / 2
	// peakEntities is what the demo's own Store reserves for. There are three.
	peakEntities = 8
)

// step is one entry of the script: at this many seconds in, say this and do the
// step of the same index.
type step struct {
	at  float64
	say string
}

// script is the whole demo. The two halves of the re-trigger are half a second
// apart rather than adjacent ticks, so that a reader watching the census sees
// the gap the removal makes.
var script = []step{
	{at: 0, say: "a listener at the origin, a looping siren 3 units to the right, and a one-shot chime heard from nowhere"},
	{at: 3, say: "the chime ran out 0.69s in - 1.10s of Clip at a rate of 1.6 - and its Entity has carried an Emitter with no Voice ever since"},
	{at: 4, say: "re-triggering is remove-then-re-add: the Emitter comes off this tick"},
	{at: 5, say: "and goes back on this one, which is a new Emitter and so a new Play"},
	{at: 7, say: "the siren is despawned mid-loop: nothing stops it, and the Voice dies with the Entity"},
	{at: 9, say: ""}, // the end of the script
}

func main() {
	cfg := map[kernel.PluginName]any{
		storage.Name: storage.Config{},
		sound.Name:   sound.Config{},
		ecs.Name:     ecs.Config{PrewarmEntities: peakEntities},
	}
	permanentfs.Configure(cfg)
	soundbackend.Configure(cfg)
	// Last of the contributors, so every plugin an override may name is in the
	// map by now.
	cfg = config.Inject(cfg)

	// storage is here because sound depends on it - a Clip named by a path is
	// read through it - even though this demo names its Clip by bytes. ecsaudio
	// needs no configuration and no Adapter of its own.
	plugins := []kernel.Plugin{
		storageplugin.New(),
		permanentfs.New(),
		appplugin.New(),
		soundplugin.New(),
		soundbackend.New(), // sound's Backend Adapter for this platform
		ecsplugin.New(),
		ecsaudioplugin.New(),
		newEmitters(),
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
const Name kernel.PluginName = "emitters"

// EmittersMainLoop is the Adapter through which this demo fills app's MainLoop
// Port. A sound demo wants no window, so the demo is its own platform.
type EmittersMainLoop kernel.Adapter[app.MainLoopPort]

// The two Systems, and the one plain subscription beside them.
type (
	// ScriptSystem spawns the world and then edits it, ordered Before the
	// binding's recording System so that every tick records the world as that
	// tick left it.
	ScriptSystem kernel.Subscription[app.UpdateEvent]
	// CensusSystem reads the two views back, ordered After the script so that
	// it describes what the script just did.
	CensusSystem kernel.Subscription[app.UpdateEvent]
	// NoteOnVoiceEnded writes down how each Voice went. The demo holds no Voice
	// handle, so this is the only way it learns that one ended at all, let
	// alone why.
	NoteOnVoiceEnded kernel.Subscription[sound.VoiceEndedEvent]
)

// Marked is the demo's own Tag, on every Entity it spawns. It is what tells
// "the Entity is gone" from "the Entity is still there and its Emitter is not",
// which is the difference the two rules here turn on and which no Component of
// the binding's can answer on its own.
type Marked struct{}

// The three Entities, one Component set each.
type (
	siren struct {
		Mark  Marked
		Place m.Transform
		Sound ecsaudio.Emitter
	}
	chime struct {
		Mark Marked
		// No Transform at all: an Emitter with none is a non-positional Voice -
		// background music, a UI click - which is what "heard from nowhere in
		// particular" already describes.
		Sound ecsaudio.Emitter
	}
	ears struct {
		Mark  Marked
		Place m.Transform
		Ears  ecsaudio.Listener
	}
)

// Log is where the ending handler writes and the census drains. It is a
// resource rather than a field on the plugin because VoiceEndedEvent is
// published inside sound's flush and dispatched without being waited on, so its
// handler runs on a goroutine the tick does not join; a resource both sides
// lock is the engine's own answer to that, and a plain field would be a race.
type Log struct{ lines []string }

func (l *Log) note(line string) { l.lines = append(l.lines, line) }

// drain takes what has been written since the last census and empties it.
func (l *Log) drain() []string {
	lines := l.lines
	l.lines = nil
	return lines
}

// emitters is the demo plugin: the gameplay, the MainLoop Adapter and the Host,
// in one type, the way cmd/sound/orbit is.
type emitters struct {
	loop app.Loop
	quit chan struct{}
	clip sound.ClipRef

	siren, chime ecs.Entity

	elapsed float64
	ticks   int
	next    int
	done    bool
}

func newEmitters() kernel.Plugin {
	return &emitters{quit: make(chan struct{}), siren: ecs.NoEntity, chime: ecs.NoEntity}
}

var (
	_ kernel.Plugin     = (*emitters)(nil)
	_ kernel.PluginHost = (*emitters)(nil)
	_ app.MainLoop      = (*emitters)(nil)
)

func (p *emitters) Name() kernel.PluginName { return Name }

// Dependencies names ecs and ecsaudio because the demo spawns Entities carrying
// the binding's Components, and sound because it locks sound's resources.
func (p *emitters) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{ecs.Name, ecsaudio.Name, sound.Name}
}

func (p *emitters) Register(registrar *kernel.Registrar, _ any) error {
	p.clip = sound.ClipWithBytes(assets.NewBlob(clipOgg))
	ecs.RegisterComponent[Marked](registrar, peakEntities)
	registrar.InitResource(&Log{})
	registrar.ProvideAdapter[EmittersMainLoop](app.MainLoop(p))

	registrar.Subscribe[ScriptSystem](ecs.ToHandler[app.UpdateEvent](registrar, p.runScript)).
		Before[ecsaudio.RecordOnUpdate]()
	registrar.Subscribe[CensusSystem](ecs.ToHandler[app.UpdateEvent](registrar, p.census)).
		After[ScriptSystem]()
	registrar.Subscribe[NoteOnVoiceEnded](p.noteOnVoiceEnded)
	return nil
}

func (p *emitters) Start(kernel.Executioner) error { return nil }
func (p *emitters) Stop(kernel.Executioner)        {}

// Attach keeps the Loop app hands over.
func (p *emitters) Attach(loop app.Loop) { p.loop = loop }

// Quit asks Run to leave its loop, guarded because it may arrive twice and from
// another goroutine.
func (p *emitters) Quit() {
	select {
	case <-p.quit:
	default:
		close(p.quit)
	}
}

// ClipboardWrite refuses: there is no window and no clipboard to write to.
func (p *emitters) ClipboardWrite(string) error {
	return fmt.Errorf("emitters: no clipboard without a window")
}

// Run is the platform loop this demo stands in for.
func (p *emitters) Run(k kernel.Executioner) error {
	if err := p.loop.Init(k); err != nil {
		return err
	}
	defer p.loop.Quit(k)

	fmt.Println("emitters: ecsaudio with no renderer - three Entities, and the Voices they own.")
	fmt.Println("          watch the chime not restart, and the siren die with its Entity.")
	fmt.Println()

	ticker := time.NewTicker(time.Second / tickRate)
	defer ticker.Stop()
	last := time.Now()
	for {
		select {
		case <-p.quit:
			return nil
		case now := <-ticker.C:
			dt := now.Sub(last).Seconds()
			last = now
			// Frame waits on every UpdateEvent it publishes, so done is written
			// and read either side of that edge rather than raced on.
			p.loop.Frame(k, dt)
			if p.done {
				return nil
			}
		}
	}
}

// sirenEmitter is the looping, positional sound. Its Falloff's Ref is the
// world's own scale rather than W3C's 1: left at 1 a source three units out
// would arrive 9.5 dB down for no reason the demo ever stated.
func (p *emitters) sirenEmitter() ecsaudio.Emitter {
	return ecsaudio.Emitter{
		Clip: p.clip,
		Params: sound.Params{
			Volume: m.Some[float32](0.70),
			Pitch:  m.Some(sirenPitch),
			Loop:   m.Some(true),
			Falloff: m.Some(sound.Falloff{
				Model: sound.DistanceInverse, Ref: sirenRange, Max: 10000, Rolloff: 1,
			}),
		},
	}
}

// chimeEmitter is the one-shot. Loop is left absent rather than set false,
// because an absent field is the default to the Play that starts the Voice and
// the default is not looping - which is the same rule sound's Params state, not
// restated by the binding.
func (p *emitters) chimeEmitter() ecsaudio.Emitter {
	return ecsaudio.Emitter{
		Clip: p.clip,
		Params: sound.Params{
			Volume: m.Some[float32](0.70),
			Pitch:  m.Some(chimePitch),
		},
	}
}

// runScript is the demo's gameplay: spawn the world, then edit it on a clock.
// Its lock set is the structural write Spawn and Despawn take, plus writes on
// the two Component Stores it edits; it reads no sound resource at all, which
// is what the binding being the only thing that talks to sound buys.
func (p *emitters) runScript(
	event app.UpdateEvent,
	world *ecs.WriteableEntities,
	sirens *ecs.Spawn[siren],
	chimes *ecs.Spawn[chime],
	listeners *ecs.Spawn[ears],
	sounds *ecs.Set[ecsaudio.Emitter],
	dropSounds *ecs.Remove[ecsaudio.Emitter],
) {
	p.elapsed += event.Dt
	for p.next < len(script) && p.elapsed >= script[p.next].at {
		at := script[p.next]
		index := p.next
		p.next++
		if at.say == "" {
			p.done = true
			fmt.Println()
			fmt.Println("emitters: the script is done.")
			return
		}
		fmt.Printf("\n%5.1fs  %s\n", at.at, at.say)
		switch index {
		case 0:
			// The Listener Tag goes on an Entity that exists to be heard from.
			// Its Transform is the whole of where the world is heard from:
			// sound never reads a camera, and neither does the binding.
			listeners.New(ears{})
			p.siren = sirens.New(siren{
				Place: m.Transform{Position: m.Vec3{X: sirenRange}},
				Sound: p.sirenEmitter(),
			})
			p.chime = chimes.New(chime{Sound: p.chimeEmitter()})
		case 1:
			// Nothing. Three seconds of a reconcile that could have re-played
			// the chime on any of a hundred and eighty ticks and did not.
		case 2:
			dropSounds.From(p.chime)
		case 3:
			sounds.UpdateFor(p.chime, p.chimeEmitter())
		case 4:
			world.Despawn(p.siren)
		}
	}
}

// census reads the two views back: what ecs holds, and what sound holds. It
// takes the Log for write because it drains what the ending handler wrote.
func (p *emitters) census(
	marks *ecs.Get[Marked],
	sounds *ecs.Get[ecsaudio.Emitter],
	voices *ecs.Read[*sound.Voices],
	device *ecs.Read[*sound.Device],
	log *ecs.Write[*Log],
) {
	for _, line := range log.Get().drain() {
		fmt.Printf("        -> %s\n", line)
	}
	p.ticks++
	if p.ticks%printEvery != 0 {
		return
	}
	live := voices.Get()
	fmt.Printf("siren %-18s chime %-18s | voices %d/%d  %s | %s\n",
		entityState(marks, sounds, p.siren), entityState(marks, sounds, p.chime),
		live.Len(), live.Cap(), voiceCensus(live), deviceLine(device.Get()))
}

// entityState is one Entity as ecs holds it, and it is three answers rather
// than two: the Entity is gone, or it is there with an Emitter, or it is there
// without one. The middle and the last look identical from sound's side - no
// Voice either way - and the whole of this demo is that they are not the same
// thing.
func entityState(marks *ecs.Get[Marked], sounds *ecs.Get[ecsaudio.Emitter], e ecs.Entity) string {
	if _, ok := marks.Of(e); !ok {
		return "despawned"
	}
	if _, ok := sounds.Of(e); !ok {
		return "alive, no Emitter"
	}
	return "alive + Emitter"
}

// voiceCensus is every live Voice as sound holds it, named by the rate it was
// commanded at. Playhead, audibility and distance are all sound's own
// arithmetic, so every one of them moves on a machine with no sound card.
func voiceCensus(live *sound.Voices) string {
	var parts []string
	for info := range live.All() {
		parts = append(parts, fmt.Sprintf("%s head %5.2fs aud %4.2f dist %4.2f",
			nameOf(info), info.Playhead, info.Audibility, info.Distance))
	}
	if len(parts) == 0 {
		return "silence"
	}
	return strings.Join(parts, " ; ")
}

// nameOf recognises a Voice by the rate the demo asked for. The binding hands
// out no handles, so this is the only identity the demo has.
func nameOf(info sound.VoiceInfo) string {
	switch info.Params.Pitch.Or(1) {
	case sirenPitch:
		return "siren"
	case chimePitch:
		return "chime"
	}
	return "?"
}

// deviceLine says whether anything can be heard. A game reads Device rather
// than handling it, and every number above moves without one.
func deviceLine(dev *sound.Device) string {
	if !dev.Ready {
		return "device: not ready (every number still moves)"
	}
	return fmt.Sprintf("device: %s %d Hz", dev.Name, dev.SampleRate)
}

// noteOnVoiceEnded records how one Voice went. It is a plain kernel
// subscription rather than an ecs System because it names no Component at all:
// the reason is on the event, and the Entity it belonged to is, by then,
// exactly the thing in question.
func (p *emitters) noteOnVoiceEnded() (kernel.Lock, kernel.Observe[sound.VoiceEndedEvent]) {
	var log kernel.Write[*Log]
	return func(access kernel.ResourceAccess) {
			log = access.GetWrite[*Log]()
		}, func(_ kernel.Kernel, event sound.VoiceEndedEvent) {
			log.Get().note("a Voice ended: " + event.Reason.String())
		}
}
