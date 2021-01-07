package player

import (
	"sync"
	"time"

	"github.com/frizinak/libym/collection"
)

type Backend interface {
	Play(string) (done chan struct{}, error error)
	Paused() bool
	Pause(bool)
	TogglePause()
	SetVolume(float64)
	IncreaseVolume(float64)
	Volume() float64
	Seek(time.Duration)
	SeekTo(float64)
	Position() float64
	Duration() time.Duration
	Stop()
}

type Player struct {
	sem     sync.Mutex
	backend Backend
	q       *collection.Queue

	current *collection.QueueItem

	err error

	seq     byte
	stopped bool
}

func NewPlayer(backend Backend, queue *collection.Queue) *Player {
	return &Player{
		backend: backend,
		q:       queue,
	}
}

func (p *Player) Err() error { return p.err }

func (p *Player) SetVolume(n float64)      { p.backend.SetVolume(n) }
func (p *Player) IncreaseVolume(n float64) { p.backend.IncreaseVolume(n) }
func (p *Player) Seek(n time.Duration)     { p.backend.Seek(n) }
func (p *Player) SeekTo(n float64)         { p.backend.SeekTo(n) }

func (p *Player) Volume() float64         { return p.backend.Volume() }
func (p *Player) Position() float64       { return p.backend.Position() }
func (p *Player) Duration() time.Duration { return p.backend.Duration() }

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

func (p *Player) ForcePlay() {
	p.sem.Lock()
	p.current = nil
	p.sem.Unlock()
	p.Play()
}

func (p *Player) Play() {
	p.sem.Lock()
	defer p.sem.Unlock()
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
		p.err = err
	}

	if !p.current.Local() {
		u, err := p.current.URL()
		if err != nil {
			p.err = err
			return
		}
		n = u.String()
	}

	done, err := p.backend.Play(n)
	if err != nil {
		p.current = nil
		p.err = err
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

func (p *Player) Pause()       { p.backend.Pause(true) }
func (p *Player) Paused() bool { return p.stopped || p.backend.Paused() }
