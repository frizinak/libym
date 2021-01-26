package collection

import (
	"strings"
	"sync"
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
}

func NewQueue() *Queue {
	l := &QueueItem{first: true}
	q := &Queue{root: l}
	q.Reset()
	return q
}

func (q *Queue) Add(s Song) {
	q.sem.Lock()
	defer q.sem.Unlock()
	q.add(s)
}

func (q *Queue) AddSlice(songs []Song) {
	q.sem.Lock()
	defer q.sem.Unlock()
	for _, s := range songs {
		q.add(s)
	}
}

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

func (q *Queue) add(s Song) {
	var last func(target, q *QueueItem)
	last = func(target, q *QueueItem) {
		if target.last {
			q.next = target
			q.prev = target.prev
			q.next.prev = q
			q.prev.next = q
			return
		}
		last(target.next, q)
	}

	item := &QueueItem{Song: s}
	if q.current != nil && q.current.last && q.current.prev == q.root {
		q.current = nil
	}

	last(q.root, item)
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

func (q *Queue) Current() *QueueItem { //(item *QueueItem, first bool, last bool) {
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
