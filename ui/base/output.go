package base

import (
	"fmt"
	"io"
	"strconv"
	"sync"

	"github.com/frizinak/libym/ui"
)

type mode int

const (
	modeNone mode = iota
	modeSongs
	modeText
)

type SimpleOutput struct {
	sem sync.Mutex
	w   io.Writer

	title string
	songs []ui.Song
	text  string

	mode mode
}

func NewSimpleOutput(w io.Writer) *SimpleOutput {
	return &SimpleOutput{w: w, mode: modeNone}
}

func (s *SimpleOutput) SetView(view ui.View)  {}
func (s *SimpleOutput) SetTitle(title string) { s.title = title }
func (s *SimpleOutput) SetSongs(l []ui.Song) {
	s.songs = l
	s.mode = modeSongs
}

func (s *SimpleOutput) SetText(str string) {
	s.text = str
	s.mode = modeText
}

func (s *SimpleOutput) AtomicFlush(cb func(ui.AtomicOutput)) {
	s.sem.Lock()
	cb(s)
	s.flush()
	s.sem.Unlock()
}

func (s *SimpleOutput) Flush() {
	s.sem.Lock()
	s.flush()
	s.sem.Unlock()
}

func (s *SimpleOutput) flush() {
	if s.mode == modeNone {
		return
	}
	fmt.Fprint(s.w, "\033[2J\033[H")
	fmt.Fprintln(s.w, s.title)
	switch s.mode {
	case modeSongs:
		f := "%" + strconv.Itoa(len(strconv.Itoa(len(s.songs)))) + "d: %s\n"
		for i, song := range s.songs {
			t := song.Title()
			if t == "" {
				t = "- unknown -"
			}
			fmt.Fprintf(s.w, f, i+1, t)
		}
	case modeText:
		fmt.Fprintln(s.w, s.text)
	}
}

func (s *SimpleOutput) Err(e error) {
	fmt.Fprintln(s.w, e.Error())
}

func (s *SimpleOutput) Errf(f string, v ...interface{}) {
	fmt.Fprintf(s.w, f, v...)
	fmt.Fprintln(s.w)
}
