// Package mpv provides a github.com/frizinak/libym/player.Backend
// implementation for both libmpv and mpv through rpc.
package mpv

import (
	"io"
	"log"
	"sync"
	"time"
)

// Backend is a generic mpv interface. Applicable to both libmpv and
// mpv through rpc.
type Backend interface {
	Init(chan<- Event) error
	Close() error

	GetPropertyDouble(string) (float64, error)
	SetPropertyDouble(string, float64) error

	SetPropertyString(string, string) error

	GetPropertyBool(string) (bool, error)
	SetPropertyBool(string, bool) error

	Command(...string) error
}

// EventID represents an mpv event type.
type EventID byte

const (
	EventEndFile EventID = 1 + iota
	EventStartFile
	EventPropertyChange
)

// Event represents an mpv event.
type Event struct {
	ID EventID
}

// New creates a new mpv wrapper that interfaces with any Backend
// implementations.
func New(log *log.Logger, backend Backend) *MPV {
	return &MPV{log: log, b: backend}
}

// MPV is an abstraction of Backend to provide the same interface to
// multiple mpv implementations.
type MPV struct {
	log *log.Logger

	sem sync.Mutex

	state struct {
		volume float64
		dones  []chan struct{}
		starts chan chan struct{}

		paused bool
		events chan Event
	}

	b Backend
}

func (m *MPV) l(err error, debug string) {
	if err != nil {
		m.log.Println(debug, err)
	}
}

// Init initializes the backend and starts listening for events.
func (m *MPV) Init() error {
	m.state.dones = make([]chan struct{}, 0)
	m.state.starts = make(chan chan struct{})
	m.state.paused = true

	m.state.events = make(chan Event, 1)
	if err := m.b.Init(m.state.events); err != nil {
		return err
	}

	vol, err := m.b.GetPropertyDouble("volume")
	m.l(err, "volume")
	m.state.volume = vol / 100

	actualPause := false
	go func() {
		for e := range m.state.events {
			switch e.ID {
			case EventEndFile:
				m.state.dones[0] <- struct{}{}
				m.state.dones = m.state.dones[1:]
			case EventStartFile:
				if !actualPause {
					m.state.paused = false
				}
				done := make(chan struct{}, 1)
				m.state.dones = append(m.state.dones, done)
				m.state.starts <- done
			case EventPropertyChange:
				paused, err := m.b.GetPropertyBool("pause")
				m.l(err, "pause")
				actualPause = paused
				m.state.paused = paused
			}
		}
	}()

	return nil
}

func (m *MPV) Close() error {
	err := m.b.Close()
	close(m.state.events)
	return err
}

func (m *MPV) Stop() {
	m.l(m.b.Command("stop"), "stop")
}

func (m *MPV) Play(file string) (chan struct{}, error) {
	m.sem.Lock()
	defer m.sem.Unlock()
	err := m.b.Command("loadfile", file, "replace")
	if err != nil {
		return nil, err
	}
	done := <-m.state.starts
	return done, nil
}

func (m *MPV) Paused() bool { return m.state.paused }

func (m *MPV) Pause(pause bool) {
	m.l(m.b.SetPropertyBool("pause", pause), "pause")
}

func (m *MPV) TogglePause() {
	n := true
	if m.state.paused {
		n = false
	}
	m.Pause(n)
}

func (m *MPV) SetVolume(n float64) {
	if n < 0 {
		n = 0
	}
	if n > 1 {
		n = 1
	}
	if err := m.b.SetPropertyDouble("volume", n*100); err != nil {
		m.l(err, "volume")
		return
	}

	m.state.volume = n
}

func (m *MPV) IncreaseVolume(n float64) {
	v := m.state.volume
	v += n
	m.SetVolume(v)
}

func (m *MPV) Volume() float64 { return m.state.volume }

func (m *MPV) Seek(adjustment time.Duration, whence int) {
	if adjustment == 0 && whence != io.SeekStart {
		return
	}

	if whence != io.SeekStart {
		cur, err := m.b.GetPropertyDouble("time-pos")
		if err != nil {
			m.l(err, "time-pos")
			return
		}

		cur += adjustment.Seconds()
		if cur < 0 {
			cur = 0
		}
		m.seek(cur)
		return
	}

	m.seek(adjustment.Seconds())
}

func (m *MPV) seek(to float64) {
	m.l(m.b.SetPropertyDouble("time-pos", to), "time-pos2")
}

func (m *MPV) SeekTo(to float64) {
	m.l(m.b.SetPropertyDouble("percent-pos", to), "percent-pos")
}

func (m *MPV) Position() time.Duration { return m.duration("time-pos") }
func (m *MPV) Duration() time.Duration { return m.duration("duration") }

func (m *MPV) duration(prop string) time.Duration {
	v, err := m.b.GetPropertyDouble(prop)
	if err != nil {
		return 0
	}

	return time.Duration(v * float64(time.Second))
}
