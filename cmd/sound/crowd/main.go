// Command crowd is the voice cap, made small enough to watch: eight Voices in
// the whole engine, one quiet piece of music, and bursts of twelve loud
// footsteps recorded in a single tick.
//
//	go run ./cmd/sound/crowd
//
// It needs no GPU and opens no window, and supplies its own app.MainLoop for
// the reason cmd/sound/orbit does.
//
// # What it is evidence for
//
// Priority is a band above audibility, and not a weight folded into it.
//
// The music here is the quietest thing in the engine - audibility 0.25 against
// a crowd at 1.00 - so a cap that ranked on loudness alone would take the music
// first, every burst, and a cap that folded priority in as a multiplier would
// take it as soon as the crowd was loud enough. Priority is compared before
// audibility is looked at, so the music at priority 1 is never the victim of a
// crowd at priority 0, however loud the crowd and however many of them arrive.
//
// The other half of that claim is the last burst, which is deliberately louder
// in the only way that counts: an alarm at priority 2. It takes the music at
// once. The band is an ordering the game states and not a protection sound
// grants to music, and a demo that only ever showed the music surviving would
// be showing luck rather than a rule.
//
// # Two things the numbers say that are worth knowing
//
// **A play that loses is stolen before it ever sounds.** Twelve plays into a
// table with seven free slots is seven Voices and five endings, and all five
// endings are ReasonStolen, in the same flush that recorded the play. Stealing
// is resolved when the play is recorded, against the table as it stood at the
// end of the last flush, so the crowd competes with what was already playing
// and not with the rest of its own burst.
//
// **Every Voice is observable exactly once.** It is in the live view now, or
// its VoiceEndedEvent has fired. The tally below is that event, counted by
// reason, which is how a demo with no ears tells "the cap took it" from "it
// played to its end".
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

// clipOgg is the demo's one sound, embedded rather than vendored into assets/.
// See ATTRIBUTION.md beside this file. Every Voice here plays it, at a
// different rate, so nothing about which Voice survives can be about which
// sound it is.
//
//go:embed pianoroll.ogg
var clipOgg []byte

// maxVoices is the whole engine's Voice cap, composed rather than defaulted:
// sound.Config{}.WithMaxVoices(8) instead of the 64 a game would ship with, so
// that a crowd small enough to print fills the table.
const maxVoices = 8

// The three priority bands this demo uses. They are plain ints a game chooses;
// sound predeclares none and 0 is the default a play gets when it names none.
const (
	priorityFootstep = 0
	priorityMusic    = 1
	priorityAlarm    = 2
)

const (
	// musicVolume is deliberately the quietest thing here. The music has to be
	// quieter than the crowd for the demo to be evidence of anything.
	musicVolume float32 = 0.25
	musicPitch  float32 = 0.50
	// crowdVolume is unity: as loud as sound will make anything without a Bus
	// above one.
	crowdVolume float32 = 1.00
	// burstSize is how many plays one tick records. It is larger than
	// maxVoices, which is the whole case: a tick that asks for more Voices
	// than exist.
	burstSize = 12
)

const (
	// tickRate is the demo's fixed update rate.
	tickRate = 60
	// printEvery is how many ticks pass between census lines.
	printEvery = tickRate / 2
)

// burst is one entry of the script: at this many seconds in, say this, and
// record burstSize plays at this priority in the one tick that crosses it.
type burst struct {
	at       float64
	say      string
	priority int
}

// script is the whole demo. The first two bursts are a crowd the music must
// survive; the third is the one thing that takes it.
var script = []burst{
	{at: 1, say: "twelve footsteps at priority 0, each four times the music's audibility", priority: priorityFootstep},
	{at: 3, say: "another twelve - a louder crowd is still a crowd, and still loses", priority: priorityFootstep},
	{at: 5, say: "twelve alarms at priority 2: a higher band, and the music goes at once", priority: priorityAlarm},
	{at: 7, say: "", priority: 0}, // the end of the script; nothing is recorded
}

func main() {
	cfg := map[kernel.PluginName]any{
		storage.Name: storage.Config{},
		// The one composed number in this demo. Everything else about sound is
		// left at its default.
		sound.Name: sound.Config{}.WithMaxVoices(maxVoices),
	}
	permanentfs.Configure(cfg)
	soundbackend.Configure(cfg)
	// Last of the contributors, so every plugin an override may name is in the
	// map by now: --cog.sound.MaxVoices=4 retunes the demo from outside it.
	cfg = config.Inject(cfg)

	// storage is here because sound depends on it - a Clip named by a path is
	// read through it - even though this demo names its Clip by bytes.
	plugins := []kernel.Plugin{
		storageplugin.New(),
		permanentfs.New(),
		appplugin.New(),
		soundplugin.New(),
		soundbackend.New(), // sound's Backend Adapter for this platform
		newCrowd(),
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
const Name kernel.PluginName = "crowd"

// CrowdMainLoop is the Adapter through which this demo fills app's MainLoop
// Port. A sound demo wants no window, so the demo is its own platform.
type CrowdMainLoop kernel.Adapter[app.MainLoopPort]

// UpdateOnTick runs the script and prints the census.
type UpdateOnTick kernel.Subscription[app.UpdateEvent]

// TallyOnVoiceEnded counts every ending by its reason. It is a subscription
// rather than a poll because a Voice the cap took is gone from the live view by
// the time anything could have looked, and the reason is the whole answer.
type TallyOnVoiceEnded kernel.Subscription[sound.VoiceEndedEvent]

// Tally is what the two handlers share, and it is a resource rather than a
// field on the plugin for one reason: VoiceEndedEvent is published inside the
// flush and dispatched without being waited on, so its handler runs on a
// goroutine the tick does not join. A resource both handlers lock for write is
// the engine's own answer to that - the kernel will not schedule them at once -
// and a plain field would be a data race.
type Tally struct {
	// counts is one running total per sound.Reason.
	counts map[sound.Reason]int
	// music is the Voice the music is playing on, written by the update
	// handler under this same lock so that the ending handler may read it.
	music sound.Voice
	// musicEnded is how the music went, if it has gone.
	musicEnded  bool
	musicReason sound.Reason
}

func (t *Tally) add(reason sound.Reason) { t.counts[reason]++ }

func (t *Tally) get(reason sound.Reason) int { return t.counts[reason] }

// crowd is the demo plugin: the gameplay, the MainLoop Adapter and the Host, in
// one type, the way cmd/sound/orbit is.
type crowd struct {
	loop app.Loop
	quit chan struct{}
	clip sound.ClipRef

	elapsed float64
	ticks   int
	next    int // the script burst to record next
	played  int // how many plays have been recorded in total
	done    bool
}

func newCrowd() kernel.Plugin { return &crowd{quit: make(chan struct{})} }

var (
	_ kernel.Plugin     = (*crowd)(nil)
	_ kernel.PluginHost = (*crowd)(nil)
	_ app.MainLoop      = (*crowd)(nil)
)

func (c *crowd) Name() kernel.PluginName { return Name }

// Dependencies names sound, whose resources this demo locks.
func (c *crowd) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{sound.Name}
}

func (c *crowd) Register(registrar *kernel.Registrar, _ any) error {
	c.clip = sound.ClipWithBytes(assets.NewBlob(clipOgg))
	registrar.InitResource(&Tally{counts: map[sound.Reason]int{}})
	registrar.ProvideAdapter[CrowdMainLoop](app.MainLoop(c))
	registrar.Subscribe[UpdateOnTick](c.updateOnTick)
	registrar.Subscribe[TallyOnVoiceEnded](c.tallyOnVoiceEnded)
	return nil
}

func (c *crowd) Start(kernel.Executioner) error { return nil }
func (c *crowd) Stop(kernel.Executioner)        {}

// Attach keeps the Loop app hands over.
func (c *crowd) Attach(loop app.Loop) { c.loop = loop }

// Quit asks Run to leave its loop, guarded because it may arrive twice and from
// another goroutine.
func (c *crowd) Quit() {
	select {
	case <-c.quit:
	default:
		close(c.quit)
	}
}

// ClipboardWrite refuses: there is no window and no clipboard to write to.
func (c *crowd) ClipboardWrite(string) error {
	return fmt.Errorf("crowd: no clipboard without a window")
}

// Run is the platform loop this demo stands in for.
func (c *crowd) Run(k kernel.Executioner) error {
	if err := c.loop.Init(k); err != nil {
		return err
	}
	defer c.loop.Quit(k)

	fmt.Printf("crowd: %d Voices in the whole engine, and bursts of %d plays in one tick.\n",
		maxVoices, burstSize)
	fmt.Println("       the music is the quietest thing here and the only one at priority 1.")
	fmt.Println()

	ticker := time.NewTicker(time.Second / tickRate)
	defer ticker.Stop()
	last := time.Now()
	for {
		select {
		case <-c.quit:
			return nil
		case now := <-ticker.C:
			dt := now.Sub(last).Seconds()
			last = now
			// Frame waits on every UpdateEvent it publishes, so done is
			// written and read either side of that edge rather than raced on.
			c.loop.Frame(k, dt)
			if c.done {
				return nil
			}
		}
	}
}

// updateOnTick starts the music on the first tick, records each burst in the
// one tick that crosses its moment, and prints the census.
//
// Its lock set is a write on the queue, a write on the Tally - shared with the
// ending handler, which is what keeps the two apart - and reads on the two
// views it reports.
func (c *crowd) updateOnTick() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var queue kernel.Write[*sound.Queue]
	var tally kernel.Write[*Tally]
	var voices kernel.Read[*sound.Voices]
	var device kernel.Read[*sound.Device]
	return func(access kernel.ResourceAccess) {
			queue = access.GetWrite[*sound.Queue]()
			tally = access.GetWrite[*Tally]()
			voices = access.GetRead[*sound.Voices]()
			device = access.GetRead[*sound.Device]()
		}, func(_ kernel.Kernel, event app.UpdateEvent) {
			q, t := queue.Get(), tally.Get()
			if t.music == sound.NoVoice && !t.musicEnded {
				// Non-positional: a cap demo has nothing to do with where a
				// sound is, and a Voice with no position is heard centred, so
				// audibility here is volume x bus and nothing else.
				t.music = q.Play(c.clip, 0, sound.Params{
					Volume:   m.Some(musicVolume),
					Pitch:    m.Some(musicPitch),
					Loop:     m.Some(true),
					Priority: m.Some(priorityMusic),
				})
				c.played++
			}
			c.elapsed += event.Dt
			c.advance(q, t)

			c.ticks++
			if c.ticks%printEvery == 0 {
				c.report(voices.Get(), t, device.Get())
			}
		}
}

// advance records every burst whose moment has passed, and ends the demo when
// the script runs out.
func (c *crowd) advance(q *sound.Queue, t *Tally) {
	for c.next < len(script) && c.elapsed >= script[c.next].at {
		at := script[c.next]
		c.next++
		if at.say == "" {
			c.done = true
			fmt.Println()
			// Two seconds have passed since the last burst, so every ending it
			// caused has been dispatched and counted by now. A summary printed
			// in the same tick as the burst would be counting a race.
			fmt.Println(c.summary(t))
			return
		}
		fmt.Printf("\n%5.1fs  %s\n", at.at, at.say)
		c.burst(q, at.priority)
	}
}

// burst records burstSize plays in this one tick. Every one of them is a
// one-shot at a rate of its own, so the crowd thins out on its own between
// bursts and the next one arrives at a table with room in it again.
func (c *crowd) burst(q *sound.Queue, priority int) {
	for i := range burstSize {
		q.Play(c.clip, 0, sound.Params{
			Volume:   m.Some(crowdVolume),
			Pitch:    m.Some(1.2 + 0.08*float32(i)),
			Priority: m.Some(priority),
		})
		c.played++
	}
}

// report prints one census line: how full the table is, what the music is
// doing, and how every Voice that has ended so far ended.
//
// Every number but the count of plays recorded comes off sound's own views. The
// live view answers as of the last flush and the Tally lags it by however long
// the dispatch of an event takes, so a burst's endings land on the line after
// it rather than the line beside it.
func (c *crowd) report(live *sound.Voices, t *Tally, dev *sound.Device) {
	fmt.Printf("live %d/%d  %s  |  %s  |  played %2d stolen %2d finished %2d  |  %s\n",
		live.Len(), live.Cap(), musicLine(live, t), bandLine(live),
		c.played, t.get(sound.ReasonStolen), t.get(sound.ReasonFinished), deviceLine(dev))
}

// musicLine is what became of the one Voice this demo is about.
func musicLine(live *sound.Voices, t *Tally) string {
	if t.musicEnded {
		return fmt.Sprintf("music %-24s", "GONE ("+t.musicReason.String()+")")
	}
	info, ok := live.Info(t.music)
	if !ok {
		// It has not been in the view since the last flush and no ending has
		// reached the Tally yet, which is the one tick the two disagree.
		return fmt.Sprintf("music %-24s", "not in this flush")
	}
	return fmt.Sprintf("music head %5.2fs aud %4.2f", info.Playhead, info.Audibility)
}

// bandLine counts the live Voices by the priority band they were played at, so
// which band filled the table is visible rather than inferred.
func bandLine(live *sound.Voices) string {
	bands := map[int]int{}
	for info := range live.All() {
		bands[info.Params.Priority.Or(0)]++
	}
	return fmt.Sprintf("footsteps %d music %d alarms %d",
		bands[priorityFootstep], bands[priorityMusic], bands[priorityAlarm])
}

// deviceLine says whether anything can be heard. A game reads Device rather
// than handling it, and every number above moves without one.
func deviceLine(dev *sound.Device) string {
	if !dev.Ready {
		return "device: not ready (every number still moves)"
	}
	return fmt.Sprintf("device: %s %d Hz", dev.Name, dev.SampleRate)
}

// tallyOnVoiceEnded counts one ending. It locks the Tally for write and nothing
// else: the reason is on the event, and a handler that went looking for the
// Voice would find it already gone.
func (c *crowd) tallyOnVoiceEnded() (kernel.Lock, kernel.Observe[sound.VoiceEndedEvent]) {
	var tally kernel.Write[*Tally]
	return func(access kernel.ResourceAccess) {
			tally = access.GetWrite[*Tally]()
		}, func(_ kernel.Kernel, event sound.VoiceEndedEvent) {
			t := tally.Get()
			t.add(event.Reason)
			if event.Voice == t.music {
				t.musicEnded, t.musicReason = true, event.Reason
				t.music = sound.NoVoice
				fmt.Printf("        -> the music ended: %s\n", event.Reason)
			}
		}
}

// summary is the last thing printed: every ending, by reason. Nothing in it is
// the demo's own bookkeeping except the count of plays it recorded, which is
// the one number sound cannot tell it.
func (c *crowd) summary(t *Tally) string {
	return fmt.Sprintf(
		"crowd: %d plays recorded; %d stolen by the cap, %d played to the end, %d stopped.",
		c.played, t.get(sound.ReasonStolen), t.get(sound.ReasonFinished), t.get(sound.ReasonStopped))
}
