// Package player provides a generic interface to play music from a
// github.com/frizinak/libym/collection.Queue.
package player

import (
	"fmt"
	"sync"
	"time"

	"github.com/frizinak/libym/collection"
)

// ErrorReporter should log errors.
type ErrorReporter interface {
	Err(error)
}

// Backend is the grittier interface to an actual music player.
type Backend interface {
	// Play should play the given string (file or url)
	// and send once on done to signal EOF.
	Play(string) (done chan struct{}, error error)

	// Paused must report if the player is paused.
	Paused() bool

	// Pause must pause the player.
	Pause(bool)

	// TogglePause should toggle the paused status.
	TogglePause()

	// SetVolume must update the player volume. argument is always
	// between 0 and 1.
	SetVolume(float64)

	// Increase volume should change the volume by the given delta.
	IncreaseVolume(float64)

	// Volume must report the current volume.
	Volume() float64

	// Seek must seek to the given duration if whence == io.SeekStart and
	// Do a relative seek if whence == io.SeekCurrent.
	Seek(d time.Duration, whence int)

	// Seek must seek to the given position. argument is always
	// between 0 and 1.
	SeekTo(float64)

	// Position must report the current position in the file.
	Position() time.Duration

	// Position must report the total file duration.
	Duration() time.Duration

	// Stop must stop playing.
	Stop()

	// Close should release as many resources as possible as the Backend
	// wont be used any more.
	Close() error
}

// Player provides an interface to play songs from a collection.Queue
// given a Backend.
type Player struct {
	sem      sync.Mutex
	backend  Backend
	reporter ErrorReporter
	q        *collection.Queue

	current *collection.QueueItem

	seq     byte
	stopped bool
}

// NewPlayer constructs a new player.
func NewPlayer(backend Backend, reporter ErrorReporter, queue *collection.Queue) *Player {
	return &Player{
		backend:  backend,
		q:        queue,
		reporter: reporter,
	}
}

// SetVolume sets the Backend volume to the given value (0-1).
func (p *Player) SetVolume(n float64) { p.backend.SetVolume(n) }

// IncreaseVolume changes the volume by the given delta (-1-1).
func (p *Player) IncreaseVolume(n float64) { p.backend.IncreaseVolume(n) }

// Seek seeks in the current file.
// whence == io.SeekStart: absolute seek
// whence == io.SeekCurrent: relative seek
func (p *Player) Seek(n time.Duration, whence int) { p.backend.Seek(n, whence) }

// SeekTo seeks to a percentage in the current file (0-1).
func (p *Player) SeekTo(n float64) { p.backend.SeekTo(n) }

// Volume returns the current volume (0-1).
func (p *Player) Volume() float64 { return p.backend.Volume() }

// Position reports the current position in the file.
func (p *Player) Position() time.Duration { return p.backend.Position() }

// Duration returns the estimated duration of the current file.
func (p *Player) Duration() time.Duration { return p.backend.Duration() }

// Next plays the next song in the queue
func (p *Player) Next() {
	p.sem.Lock()
	p.current = nil
	n := p.q.Next()
	p.sem.Unlock()
	if n.IsBeyondLast() {
		p.q.Prev()
		return
	}
	p.Play()
}

// Prev plays the previous song in the queue
func (p *Player) Prev() {
	p.sem.Lock()
	p.current = nil
	n := p.q.Prev()
	p.sem.Unlock()
	if n.IsBeyondFirst() {
		p.q.Next()
		return
	}
	p.Play()
}

// ForcePlay as opposed to play, restarts playback of the active queue item.
// e.g.: if song A is currently playing and the active queue item is set to
// item B, B would be start after A is done playing. ForcePlay stops A and
// starts B.
func (p *Player) ForcePlay() {
	p.sem.Lock()
	p.current = nil
	p.sem.Unlock()
	p.Play()
}

func (p *Player) songErr(qi *collection.QueueItem, err error) {
	if qi == nil {
		p.reporter.Err(err)
		return
	}

	p.reporter.Err(fmt.Errorf("[%s-%s] %s: %w", qi.NS(), qi.ID(), qi.Title(), err))
}

// Play starts playback if nothing was playing or the player was paused.
func (p *Player) Play() {
	p.sem.Lock()
	defer p.sem.Unlock()
	p.play()
}

func (p *Player) play() {
	if p.Paused() {
		p.stopped = false
		p.backend.Pause(false)
	}

	if p.current != nil {
		return
	}

	p.seq++
	seq := p.seq
	p.current = p.q.Current()
	if p.current == nil || p.current.IsBeyondFirst() || p.current.IsBeyondLast() {
		p.stopped = true
		p.current = nil
		p.backend.Stop()
		return
	}

	n, err := p.current.File()
	if err != nil {
		p.songErr(p.current, err)
		p.current = nil
		p.q.Next()
		p.play()
		return
	}

	if !p.current.Local() {
		u, err := p.current.URL()
		if err != nil {
			p.songErr(p.current, err)
			p.current = nil
			p.q.Next()
			p.play()
			return
		}
		n = u.String()
	}

	done, err := p.backend.Play(n)
	if err != nil {
		p.songErr(p.current, err)
		p.current = nil
		p.q.Next()
		p.play()
		return
	}

	go func() {
		<-done
		p.sem.Lock()
		play := false
		if p.seq == seq {
			p.current = nil
			n := p.q.Next()
			play = !n.IsBeyondLast()
		}
		p.sem.Unlock()
		if play && !p.Paused() {
			p.Play()
		}
	}()
}

// Pause pauses the player.
func (p *Player) Pause() { p.backend.Pause(true) }

// Paused reports the paused state.
func (p *Player) Paused() bool { return p.stopped || p.backend.Paused() }

// Close releases resources and this player-backend pair should not be used
// anymore.
func (p *Player) Close() error { return p.backend.Close() }
