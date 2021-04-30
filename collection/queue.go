package collection

import (
	"math/rand"
	"strings"
	"sync"
	"time"
)

type QueueItem struct {
	prev, next *QueueItem
	Song

	first, last bool
}

func (q *QueueItem) Prev() *QueueItem    { return q.prev }
func (q *QueueItem) Next() *QueueItem    { return q.next }
func (q *QueueItem) IsBeyondFirst() bool { return q.first }
func (q *QueueItem) IsBeyondLast() bool  { return q.last }

type Queue struct {
	sem     sync.RWMutex
	root    *QueueItem
	current *QueueItem
	r       *rand.Rand
}

func NewQueue() *Queue {
	l := &QueueItem{first: true}
	q := &Queue{root: l}
	q.r = rand.New(rand.NewSource(time.Now().UnixNano()))
	q.Reset()

	return q
}

func (q *Queue) Add(ix int, s Song) {
	q.sem.Lock()
	defer q.sem.Unlock()
	q.add(ix, s)
}

func (q *Queue) AddSlice(ix int, songs []Song) {
	q.sem.Lock()
	defer q.sem.Unlock()
	for _, s := range songs {
		q.add(ix, s)
		if ix >= 0 {
			ix++
		}
	}
}

// ShuffleRange shuffles items in the queue in range [start, end]
// if start < 0: shuffle from beginning
// if end < 0: shuffle until the end
// thus ShuffleRange(-1, -1) shuffles the entire queue
func (q *Queue) ShuffleRange(start, end int) {
	if start < 0 {
		start = 0
	}
	q.sem.Lock()
	defer q.sem.Unlock()
	l := make([]*QueueItem, 0, 1)
	c := q.root
	n := 0
	var first, last *QueueItem
	for c != nil {
		if c.first || c.last {
			if c.first {
				first = c
			} else if c.last {
				last = c
			}
			c = c.next
			continue
		}
		if n == start-1 {
			first = c
		} else if n == end+1 && end >= 0 {
			last = c
			break
		} else if n >= start && (n <= end || end < 0) {
			l = append(l, c)
		}

		c = c.next
		n++
	}

	if last == nil && end < 0 {
		last = &QueueItem{last: true}
	}

	if first == nil || last == nil {
		panic("shuffle failed")
	}

	q.r.Shuffle(len(l), func(i, j int) {
		l[i], l[j] = l[j], l[i]
	})

	if len(l) <= 1 {
		return
	}

	first.next = l[0]
	l[0].prev = first
	for i := 0; i < len(l)-1; i++ {
		l[i].next = l[i+1]
		l[i+1].prev = l[i]
	}
	l[len(l)-1].next = last
	last.prev = l[len(l)-1]
}

func (q *Queue) Shuffle() { q.ShuffleRange(0, -1) }

func (q *Queue) Slice() []Song {
	q.sem.RLock()
	defer q.sem.RUnlock()
	return q.slice()
}

func (q *Queue) slice() []Song {
	l := make([]Song, 0)
	c := q.root
	for c != nil {
		if c.first || c.last {
			c = c.next
			continue
		}
		l = append(l, c)
		c = c.next
	}

	return l
}

func (q *Queue) String() string {
	s := q.Slice()
	l := make([]string, 0, len(s))
	for _, song := range s {
		l = append(l, song.Title())
	}
	return strings.Join(l, "\n")
}

func (q *Queue) add(ix int, s Song) {
	var last func(int, *QueueItem, *QueueItem)
	last = func(index int, target, q *QueueItem) {
		if target.last || index == ix {
			q.next = target
			q.prev = target.prev
			q.next.prev = q
			q.prev.next = q
			return
		}
		last(index+1, target.next, q)
	}

	item := &QueueItem{Song: s}
	if q.current != nil && q.current.last && q.current.prev == q.root {
		q.current = nil
	}

	last(0, q.root, item)
}

func (q *Queue) SetCurrentIndex(i int) {
	q.sem.RLock()
	defer q.sem.RUnlock()
	c := q.root.next
	for n := 0; n < i; n++ {
		if c.last {
			c = c.prev
			break
		}
		c = c.next
	}

	q.current = c
}

func (q *Queue) CurrentIndex() int {
	q.sem.RLock()
	defer q.sem.RUnlock()

	i := -1
	c := q.root.next
	for c != nil {
		if c.first || c.last {
			return -1
		}
		i++
		if c == q.current {
			return i
		}
		c = c.next
	}

	return i
}

func (q *Queue) Current() *QueueItem {
	q.sem.RLock()
	defer q.sem.RUnlock()

	if q.current == nil {
		q.current = q.root.next
	}

	return q.current
}

func (q *Queue) Prev() *QueueItem {
	q.sem.RLock()
	defer q.sem.RUnlock()

	if q.current == nil {
		q.current = q.root
	}

	p := q.current.prev
	if p == nil {
		return q.current
	}

	q.current = p

	return q.current
}

func (q *Queue) Next() *QueueItem {
	q.sem.RLock()
	defer q.sem.RUnlock()

	if q.current == nil {
		q.current = q.root.next
	}

	p := q.current.next
	if p == nil {
		return q.current
	}

	q.current = p
	return q.current
}

func (q *Queue) Reset() {
	q.sem.Lock()
	defer q.sem.Unlock()
	q.root = &QueueItem{first: true, next: &QueueItem{last: true}}
	q.root.next.prev = q.root
	q.current = nil
}
