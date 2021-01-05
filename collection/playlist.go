package collection

import (
	"fmt"
	"net/url"
	"strings"
	"sync"

	"github.com/frizinak/binary"
)

type Song interface {
	IDer
	Title() string
	Local() bool
	URL() (*url.URL, error)
	File() (string, error)
	Marshal(*binary.Writer) error
}

type IDer interface {
	NS() string
	ID() string
	GlobalID() string
}

func GlobalID(i IDer) string { return fmt.Sprintf("%s-%s", i.NS(), i.ID()) }

type Playlist struct {
	sem   sync.RWMutex
	name  string
	songs []Song
}

func NewPlaylist(name string) *Playlist {
	return &Playlist{name: name, songs: make([]Song, 0)}
}

func (p *Playlist) Search(q string) []Song {
	l := p.List()
	qs := strings.Fields(strings.ToLower(q))
	a := make([]Song, 0)
	var all bool
	for _, s := range l {
		all = true
		for _, q := range qs {
			if !strings.Contains(strings.ToLower(s.Title()), q) {
				all = false
				break
			}
		}
		if all {
			a = append(a, s)
		}
	}

	return a
}

func (p *Playlist) List() []Song {
	p.sem.RLock()
	n := make([]Song, len(p.songs))
	copy(n, p.songs)
	p.sem.RUnlock()
	return n
}

func (p *Playlist) Add(s Song) {
	p.sem.Lock()
	defer p.sem.Unlock()
	id := s.GlobalID()
	for i, song := range p.songs {
		gid := song.GlobalID()
		if gid == id {
			p.songs = append(p.songs[:i], p.songs[i+1:]...)
			p.songs = append(p.songs, song)
			return
		}
	}
	p.songs = append(p.songs, s)
}

func (p *Playlist) Del(s Song) {
	p.sem.Lock()
	defer p.sem.Unlock()
	ix := -1
	for i, _s := range p.songs {
		if _s == s {
			ix = i
			break
		}
	}
	if ix == -1 {
		return
	}

	p.songs = append(p.songs[:ix], p.songs[ix+1:]...)
}

func (p *Playlist) DelIndexes(ix []int) {
	songs := make([]Song, 0, len(ix))
	p.sem.RLock()
	for _, i := range ix {
		if i < 0 || i >= len(p.songs) {
			continue
		}
		songs = append(songs, p.songs[i])
	}
	p.sem.RUnlock()
	for _, s := range songs {
		p.Del(s)
	}
}

func (p *Playlist) Move(from, to Song) {
	p.sem.Lock()
	defer p.sem.Unlock()

	var f, t int
	for i, _s := range p.songs {
		if _s == from {
			f = i
		}
		if _s == to {
			t = i
		}
	}

	if t == f {
		return
	}

	if t > f {
		t--
	}

	// delete 'from' song
	p.songs = append(p.songs[:f], p.songs[f+1:]...)
	// shift all starting at 'to' song
	p.songs = append(p.songs[:t+1], p.songs[t:]...)
	p.songs[t] = from
}

func (p *Playlist) MoveIndex(from []int, to int) {
	froms := make([]Song, 0, len(from))
	p.sem.RLock()
	//if from < 0 || from >= len(p.songs) || to < 0 || to >= len(p.songs) {
	for _, f := range from {
		if f < 0 || f >= len(p.songs) {
			continue
		}
		froms = append(froms, p.songs[f])
	}

	if to >= len(p.songs) {
		p.sem.RUnlock()
		return
	}

	t := p.songs[to]
	p.sem.RUnlock()
	for _, f := range froms {
		p.Move(f, t)
	}
}

func (p *Playlist) Queue(q *Queue) {
	p.sem.RLock()
	defer p.sem.RUnlock()

	q.AddSlice(p.songs)
}

func (p *Playlist) QueueSelection(q *Queue, sel []int) {
	songs := make([]Song, 0, len(sel))
	p.sem.RLock()
	defer p.sem.RUnlock()

	for _, i := range sel {
		if i < 0 || i >= len(p.songs) {
			continue
		}

		songs = append(songs, p.songs[i])
	}

	q.AddSlice(songs)
}
