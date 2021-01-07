package collection

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/frizinak/binary"
	"github.com/frizinak/libym/youtube"
)

var (
	ErrNotExists = errors.New("playlist does not exist")
	ErrExists    = errors.New("playlist already exists")
)

func IsErrNotExists(err error) bool { return errors.Is(err, ErrNotExists) }
func IsErrExists(err error) bool    { return errors.Is(err, ErrExists) }

type Collection struct {
	sem       sync.RWMutex
	dir       string
	playlists map[string]*Playlist
	q         *Queue

	l          *log.Logger
	concurrent int

	needsSave chan struct{}
	autoSave  bool

	unmarshalers map[string]Unmarshaler

	newSong chan Song
}

func New(l *log.Logger, dir string, queue *Queue, concurrentDownloads int, autoSave bool) *Collection {
	return &Collection{
		dir:       dir,
		playlists: make(map[string]*Playlist),
		q:         queue,

		l:          l,
		concurrent: concurrentDownloads,

		autoSave:  autoSave,
		needsSave: make(chan struct{}, 0),

		unmarshalers: make(map[string]Unmarshaler, 0),
	}
}

func (c *Collection) pathDB() string    { return filepath.Join(c.dir, "db") }
func (c *Collection) pathSongs() string { return filepath.Join(c.dir, "songs") }
func (c *Collection) pathSong(id IDer) string {
	sum := sha256.Sum256([]byte(id.ID()))
	l := base64.RawURLEncoding.EncodeToString(sum[:])

	dir := filepath.Join(
		c.pathSongs(),
		id.NS(),
		string(l[0]),
		string(l[1]),
	)

	os.MkdirAll(dir, 0755)
	return filepath.Join(dir, l)
}

func (c *Collection) Init() error {
	os.MkdirAll(c.dir, 0755)
	c.RegisterUnmarshaler(
		NSYoutube,
		func(dec *binary.Reader) (Song, error) {
			id := dec.ReadString(8)
			title := dec.ReadString(16)
			if err := dec.Err(); err != nil {
				return nil, err
			}
			s := c.FromYoutube(youtube.NewResult(id, title))
			return s, nil
		},
	)

	loading := true
	go func() {
		newAfter := func() <-chan time.Time {
			return time.After(time.Second * 5)
		}
		after := newAfter()
		needs := false
		lastQueueIndex := -1
		for {
			select {
			case <-c.needsSave:
				if loading {
					break
				}
				needs = true
			case <-after:
				after = newAfter()
				if !c.autoSave {
					continue
				}
				ix := c.q.CurrentIndex()
				if ix != lastQueueIndex {
					needs = true
					lastQueueIndex = ix
				}

				if !needs {
					continue
				}
				needs = false
				if err := c.Save(); err != nil {
					panic(err)
				}
			}
		}
	}()

	workers := c.concurrent
	workDownloads := make(chan Song, workers)
	workTitles := make(chan Song, workers)
	go func() {
		for w := range workTitles {
			if w.Title() != "" {
				continue
			}
			y, ok := w.(*YoutubeSong)
			if !ok {
				continue
			}

			if err := y.r.UpdateTitle(); err != nil {
				c.l.Println("Title err:", err)
				continue
			}
			c.changed()
		}
	}()

	for i := 0; i < workers; i++ {
		go func() {
			for w := range workDownloads {
				do := func() error {
					file, err := w.File()
					if err != nil {
						return err
					}
					u, err := w.URL()
					if err != nil {
						return err
					}
					tmp := TempFile(file)
					f, err := os.Create(tmp)
					if err != nil {
						return err
					}

					err = DownloadAudio(f, u)
					f.Close()
					if err != nil {
						os.Remove(tmp)
						return err
					}
					return os.Rename(tmp, file)
				}

				c.l.Printf("Downloading %s", w.Title())
				if err := do(); err != nil {
					c.l.Println("Download err:", err.Error(), w.Title())
					continue
				}
				c.l.Printf("Downloaded %s", w.Title())
			}
		}()
	}

	eachSong := func(cb func(s Song)) {
		for _, s := range c.q.Slice() {
			cb(s)
		}
		for _, s := range c.Songs() {
			cb(s)
		}
	}

	addTitle := func(s Song) {
		workTitles <- s
	}

	var mapsem sync.Mutex
	startedDownload := make(map[string]struct{})
	addDownload := func(s Song) {
		id := s.GlobalID()
		if s.Local() {
			mapsem.Lock()
			delete(startedDownload, id)
			mapsem.Unlock()
			return
		}

		mapsem.Lock()
		if _, ok := startedDownload[id]; ok {
			mapsem.Unlock()
			return
		}

		startedDownload[id] = struct{}{}
		mapsem.Unlock()
		workDownloads <- s
	}

	c.newSong = make(chan Song, workers)
	go func() {
		for s := range c.newSong {
			if loading {
				continue
			}
			go func(s Song) {
				addTitle(s)
				addDownload(s)
			}(s)
		}
	}()

	if err := c.Load(); err != nil {
		return err
	}
	loading = false

	go func() {
		since := time.Time{}
		for {
			if time.Since(since) < time.Second*3600 {
				time.Sleep(time.Second * 10)
				continue
			}
			since = time.Now()
			eachSong(addTitle)
		}
	}()

	go func() {
		since := time.Time{}
		for {
			if time.Since(since) < time.Second*3600 {
				time.Sleep(time.Second * 10)
				continue
			}
			since = time.Now()
			eachSong(addDownload)
		}
	}()

	return nil
}

func (c *Collection) changed() {
	c.needsSave <- struct{}{}
}

func (c *Collection) clean(playlist string) string {
	return strings.TrimSpace(strings.ToLower(playlist))
}

func (c *Collection) Create(n string) error {
	n = c.clean(n)
	if n == "" {
		return fmt.Errorf("invalid playlist name '%s'", n)
	}

	c.sem.Lock()
	defer c.sem.Unlock()

	if _, ok := c.playlists[n]; ok {
		return ErrExists
	}

	c.playlists[n] = NewPlaylist(n)
	c.changed()

	return nil
}

func (c *Collection) Delete(n string) error {
	n = c.clean(n)

	c.sem.Lock()
	defer c.sem.Unlock()
	if _, ok := c.playlists[n]; !ok {
		return ErrNotExists
	}

	delete(c.playlists, n)
	c.changed()
	return nil
}

func (c *Collection) get(n string) (*Playlist, error) {
	n = c.clean(n)
	c.sem.RLock()
	p, ok := c.playlists[n]
	c.sem.RUnlock()
	if !ok {
		return nil, ErrNotExists
	}
	return p, nil
}

func (c *Collection) Search(q string) []Song {
	a := make([]Song, 0)
	c.sem.RLock()
	for _, p := range c.playlists {
		a = append(a, p.Search(q)...)
	}
	c.sem.RUnlock()

	uniq := make(map[string]struct{}, len(a))
	list := make([]Song, 0, len(a))
	for _, s := range a {
		gid := s.GlobalID()
		if _, ok := uniq[gid]; !ok {
			uniq[gid] = struct{}{}
			list = append(list, s)
		}

	}

	return list
}

func (c *Collection) List() []string {
	c.sem.RLock()
	n := make([]string, 0, len(c.playlists))
	for i := range c.playlists {
		n = append(n, i)
	}
	c.sem.RUnlock()
	return n
}

func (c *Collection) Songs() []Song {
	n := make([]Song, 0)
	c.sem.RLock()
	for _, p := range c.playlists {
		n = append(n, p.List()...)
	}
	c.sem.RUnlock()
	return n
}

func (c *Collection) PlaylistSongs(playlist string) ([]Song, error) {
	p, err := c.get(playlist)
	if err != nil {
		return nil, err
	}
	return p.List(), nil
}

func (c *Collection) AddSong(playlist string, s Song) error {
	p, err := c.get(playlist)
	if err != nil {
		return err
	}
	p.Add(s)
	c.newSong <- s
	c.changed()
	return nil
}

func (c *Collection) DelSong(playlist string, s Song) error {
	p, err := c.get(playlist)
	if err != nil {
		return err
	}
	p.Del(s)
	c.changed()
	return nil
}

func (c *Collection) DelSongIndexes(playlist string, ix []int) error {
	p, err := c.get(playlist)
	if err != nil {
		return err
	}
	p.DelIndexes(ix)
	c.changed()
	return nil
}

func (c *Collection) MoveSongIndex(playlist string, from []int, to int) error {
	p, err := c.get(playlist)
	if err != nil {
		return err
	}
	p.MoveIndex(from, to)
	c.changed()
	return nil
}

func (c *Collection) Queue(n string) error {
	p, err := c.get(n)
	if err != nil {
		return err
	}
	p.Queue(c.q)
	c.changed()
	return nil
}

func (c *Collection) QueueSelection(n string, sel []int) error {
	p, err := c.get(n)
	if err != nil {
		return err
	}
	p.QueueSelection(c.q, sel)
	c.changed()
	return nil
}

func (c *Collection) QueueSong(s Song) {
	c.q.Add(s)
	c.changed()
}

func (c *Collection) QueueNewSong(s Song) {
	c.QueueSong(s)
	c.newSong <- s
}

func (c *Collection) FromYoutube(r *youtube.Result) *YoutubeSong {
	y := &YoutubeSong{r: r}
	y.file = c.pathSong(y)
	return y
}

func (c *Collection) FromYoutubeURL(url, title string) (*YoutubeSong, error) {
	y, err := youtube.FromURL(url, title)
	if err != nil {
		return nil, err
	}
	// if err := y.UpdateTitle(); err != nil {
	// 	return nil, err
	// }

	return c.FromYoutube(y), nil
}
