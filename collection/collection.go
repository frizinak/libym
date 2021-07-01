package collection

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/frizinak/binary"
	"github.com/frizinak/libym/youtube"
)

var (
	ErrNotExists     = errors.New("playlist does not exist")
	ErrSongNotExists = errors.New("song does not exist")
	ErrExists        = errors.New("playlist already exists")
)

var nsRE = regexp.MustCompile(`[^a-zA-Z0-9]+`)

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
	running bool

	problematics *Problematics
}

func New(l *log.Logger, dir string, queue *Queue, concurrentDownloads int, autoSave bool) *Collection {
	return &Collection{
		dir:       dir,
		playlists: make(map[string]*Playlist),
		q:         queue,

		l:          l,
		concurrent: concurrentDownloads,

		autoSave:  autoSave,
		needsSave: make(chan struct{}),

		unmarshalers: make(map[string]Unmarshaler),
		problematics: NewProblematics(),
	}
}

func (c *Collection) pathDB() string    { return filepath.Join(c.dir, "db") }
func (c *Collection) pathSongs() string { return filepath.Join(c.dir, "songs") }
func (c *Collection) globSongs() string {
	return filepath.Join(c.pathSongs(), "*", "*", "*", "*")
}

func (c *Collection) SongPath(id IDer) string {
	sum := sha256.Sum256([]byte(id.ID()))
	l := base64.RawURLEncoding.EncodeToString(sum[:])
	ns := nsRE.ReplaceAllString(id.NS(), "-")
	dir := filepath.Join(c.pathSongs(), ns, string(l[0]), string(l[1]))

	return filepath.Join(dir, l)
}

func (c *Collection) Init() error {
	os.MkdirAll(c.dir, 0o755)
	c.newSong = make(chan Song, c.concurrent)
	done := make(chan struct{}, 1)
	go func() {
		for range c.newSong {
			// ignore while loading
		}
		done <- struct{}{}
	}()

	c.RegisterUnmarshaler(
		NSYoutube,
		func(dec *binary.Reader) (Song, error) {
			return YoutubeSongUnmarshal(c, dec)
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

	if err := c.Load(); err != nil {
		return err
	}
	loading = false
	close(c.newSong)
	<-done
	c.newSong = nil

	return nil
}

func (c *Collection) Run(ratelimitDownloads, ratelimitMeta <-chan struct{}) {
	c.sem.Lock()
	if c.running {
		c.sem.Unlock()
		return
	}
	c.running = true
	c.sem.Unlock()
	c.newSong = make(chan Song, c.concurrent)

	g, _ := filepath.Glob(c.globSongs() + ".tmp")
	for _, p := range g {
		os.Remove(p)
	}

	var mapsem sync.Mutex
	startedDownload := make(map[string]struct{})

	taskDownloads := NewSongTasks(
		c.concurrent,
		ratelimitDownloads,
		func(s Song) bool {
			id := GlobalID(s)
			if s.Local() {
				mapsem.Lock()
				delete(startedDownload, id)
				mapsem.Unlock()
				return false
			}

			mapsem.Lock()
			if _, ok := startedDownload[id]; ok {
				mapsem.Unlock()
				return false
			}

			startedDownload[id] = struct{}{}
			mapsem.Unlock()
			return true
		},
		func(s Song) {
			do := func() error {
				file, err := s.File()
				os.MkdirAll(filepath.Dir(file), 0o755)
				if err != nil {
					return err
				}
				u, err := s.URL()
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

			c.l.Printf("Downloading %s:%s %s", s.NS(), s.ID(), s.Title())
			if err := do(); err != nil {
				c.problematics.Add(s, err)
				c.l.Println("Download err:", err.Error(), s.NS(), s.ID(), s.Title())
				return
			}
			c.l.Printf("Downloaded %s:%s %s", s.NS(), s.ID(), s.Title())
		},
	)
	taskMeta := NewSongTasks(
		c.concurrent,
		ratelimitMeta,
		func(s Song) bool {
			return s.Title() == ""
		},
		func(s Song) {
			if err := s.UpdateTitle(); err != nil {
				c.problematics.Add(s, err)
				c.l.Println("Title err:", err)
				return
			}
			c.l.Printf("Updated title: %s:%s %s", s.NS(), s.ID(), s.Title())
			c.changed()
		},
	)

	taskDownloads.Start()
	taskMeta.Start()

	eachSong := func(cb func(s Song)) {
		for _, s := range c.q.Slice() {
			cb(s)
		}
		for _, s := range c.Songs() {
			cb(s)
		}
	}

	go func() {
		for s := range c.newSong {
			taskDownloads.Add(s)
			taskMeta.Add(s)
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
			eachSong(taskMeta.Add)
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
			eachSong(taskDownloads.Add)
		}
	}()
}

func (c *Collection) changed() {
	c.needsSave <- struct{}{}
}

func (c *Collection) clean(playlist string) string {
	return strings.TrimSpace(strings.ToLower(playlist))
}

func (c *Collection) Problematics() *Problematics { return c.problematics }

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

func (c *Collection) Exists(n string) bool {
	c.sem.RLock()
	_, ok := c.playlists[n]
	c.sem.RUnlock()
	return ok
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

type SearchResult struct {
	Song
	Playlists []string
}

func (c *Collection) Search(q string) []*SearchResult {
	byS := make(map[string]*SearchResult)
	a := make([]Song, 0)

	c.sem.RLock()
	for name, p := range c.playlists {
		res := p.Search(q)
		if len(res) == 0 {
			continue
		}
		for _, s := range res {
			gid := GlobalID(s)
			if _, ok := byS[gid]; !ok {
				byS[gid] = &SearchResult{s, make([]string, 0, 1)}
			}
			byS[gid].Playlists = append(byS[gid].Playlists, name)
		}

		a = append(a, res...)
	}
	c.sem.RUnlock()

	uniq := make(map[string]struct{}, len(a))
	list := make([]*SearchResult, 0, len(a))
	for _, s := range a {
		gid := GlobalID(s)
		if _, ok := uniq[gid]; !ok {
			uniq[gid] = struct{}{}
			list = append(list, byS[gid])
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

func (c *Collection) FindAll(ns, id string) (Song, []string, error) {
	var song Song
	pls := make([]string, 0)
	c.sem.RLock()
	for name, p := range c.playlists {
		s, err := p.Find(ns, id)
		if err != nil {
			continue
		}
		pls = append(pls, name)
		if song == nil {
			song = s
		}
	}
	c.sem.RUnlock()
	sort.Strings(pls)

	if song == nil {
		return nil, pls, ErrSongNotExists
	}

	return song, pls, nil
}

func (c *Collection) Find(ns, id string) (Song, error) {
	var song Song
	c.sem.RLock()
	for _, p := range c.playlists {
		s, err := p.Find(ns, id)
		if err == nil {
			song = s
			break
		}
	}
	c.sem.RUnlock()
	if song == nil {
		return nil, ErrSongNotExists
	}

	return song, nil
}

func (c *Collection) PlaylistSongs(playlist string) ([]Song, error) {
	p, err := c.get(playlist)
	if err != nil {
		return nil, err
	}
	return p.List(), nil
}

func (c *Collection) AddSong(playlist string, s Song, reappend bool) error {
	p, err := c.get(playlist)
	if err != nil {
		return err
	}
	p.Add(s, reappend)
	if c.newSong != nil {
		c.newSong <- s
	}
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

func (c *Collection) Queue(ix int, n string) error {
	p, err := c.get(n)
	if err != nil {
		return err
	}
	p.Queue(c.q, ix)
	c.changed()
	return nil
}

func (c *Collection) QueueSong(ix int, s Song) {
	c.q.Add(ix, s)
	if c.newSong != nil {
		c.newSong <- s
	}
	c.changed()
}

func (c *Collection) RenameSong(s Song, name string) {
	s.SetTitle(name)
	c.changed()
}

func (c *Collection) FromYoutube(r *youtube.Result) *YoutubeSong {
	y := &YoutubeSong{Result: r}
	y.file = c.SongPath(y)
	return y
}

func (c *Collection) FromYoutubeURL(url, title string) (*YoutubeSong, error) {
	y, err := youtube.FromURL(url, title)
	if err != nil {
		return nil, err
	}

	return c.FromYoutube(y), nil
}

func (c *Collection) UnreferencedDownloads() []string {
	g, err := filepath.Glob(c.globSongs())
	if err != nil {
		panic(err) // filepath.Glob only returns pattern errors
	}

	gm := make(map[string]struct{}, len(g))
	for _, p := range g {
		gm[p] = struct{}{}
	}

	songs := c.Songs()
	songs = append(songs, c.q.Slice()...)
	for _, s := range songs {
		delete(gm, c.SongPath(s))
	}

	list := make([]string, 0, len(gm))
	for i := range gm {
		list = append(list, i)
	}

	return list
}
