package collection

import (
	"errors"
	"sort"
	"sync"
)

var ErrUnknown = errors.New("unknown")

type Problematic struct {
	s      Song
	reason error
}

func (p Problematic) Song() Song { return p.s }
func (p Problematic) Reason() error {
	if p.reason == nil {
		return ErrUnknown
	}
	return p.reason
}

type problematicList []Problematic

func (p problematicList) Len() int           { return len(p) }
func (p problematicList) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }
func (p problematicList) Less(i, j int) bool { return GlobalID(p[i].s) < GlobalID(p[j].s) }

type Problematics struct {
	rw sync.RWMutex
	m  map[string]Problematic
}

func NewProblematics() *Problematics {
	return &Problematics{m: make(map[string]Problematic)}
}

func (p *Problematics) Reason(s IDer) string {
	p.rw.RLock()
	entry, ok := p.m[GlobalID(s)]
	p.rw.RUnlock()
	if !ok {
		return ""
	}
	return entry.Reason().Error()
}

func (p *Problematics) Add(s Song, err error) {
	p.rw.Lock()
	p.m[GlobalID(s)] = Problematic{s, err}
	p.rw.Unlock()
}

func (p *Problematics) Del(s IDer) {
	p.rw.Lock()
	delete(p.m, GlobalID(s))
	p.rw.Unlock()
}

func (p *Problematics) List() []Problematic {
	l := make(problematicList, 0, len(p.m))
	p.rw.RLock()
	for _, v := range p.m {
		l = append(l, v)
	}
	p.rw.RUnlock()
	sort.Sort(l)
	return l
}
