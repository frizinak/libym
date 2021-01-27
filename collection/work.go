package collection

import (
	"sync"
)

// SongTasks runs a bunch of tasks on a song concurrenly
// with a given ratelimiter.
type SongTasks struct {
	concurrency int

	rate <-chan struct{}

	qrw   sync.RWMutex
	queue chan Song

	filter func(Song) bool
	cb     func(Song)
}

// NewSongTasks creates a new SongTask that will execute cb() for every item
// that returns true when passed through filter().
// filter will also be executed concurrenly but never ratelimited.
func NewSongTasks(
	concurrency int,
	rate <-chan struct{},
	filter func(Song) bool,
	cb func(Song),
) *SongTasks {
	return &SongTasks{
		concurrency: concurrency,
		rate:        rate,
		queue:       make(chan Song, concurrency),
		filter:      filter,
		cb:          cb,
	}
}

func (t *SongTasks) Start() {
	list := make([]Song, 0)

	for i := 0; i < t.concurrency; i++ {
		go func() {
			for s := range t.queue {
				if !t.filter(s) {
					continue
				}

				t.qrw.Lock()
				list = append(list, s)
				t.qrw.Unlock()
			}
		}()

		go func() {
			for range t.rate {
				t.qrw.RLock()
				l := len(list)
				t.qrw.RUnlock()
				if l == 0 {
					continue
				}

				t.qrw.Lock()
				if len(list) == 0 {
					t.qrw.Unlock()
					continue
				}
				s := list[0]
				list = list[1:]
				t.qrw.Unlock()

				t.cb(s)
			}
		}()
	}
}

func (t *SongTasks) Add(s Song) {
	t.queue <- s
}
