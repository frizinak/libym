package base

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/frizinak/libym/collection"
	"github.com/frizinak/libym/player"
	"github.com/frizinak/libym/scraper"
	"github.com/frizinak/libym/ui"
	"github.com/frizinak/libym/youtube"
)

var viewNames = map[ui.View]string{
	ui.ViewQueue:     "queue",
	ui.ViewSearch:    "search",
	ui.ViewSearchOwn: "search for local songs",
	ui.ViewPlaylist:  "playlist",
	ui.ViewPlaylists: "playlists",
	ui.ViewHelp:      "help",
	ui.ViewJobs:      "jobs",
	ui.ViewExternal:  "external",
}

type Can byte

const (
	CanSong Can = iota
	CanSearchResult
	CanSongRemove
	CanMove
	CanQueue
	CanCancelJob
)

type Job struct {
	id       string
	Name     string
	Progress float64
	cancel   func()
}

func (j *Job) ID() string              { return j.id }
func (j *Job) SetCancel(cancel func()) { j.cancel = cancel }
func (j *Job) Cancel() {
	c := j.cancel
	if c != nil {
		c()
		j.cancel = nil
	}
}

type Jobs struct {
	mutex sync.RWMutex
	j     map[string]*Job
	n     uint64
}

func (j *Jobs) Add(name string) *Job {
	j.mutex.Lock()
	j.n++
	jobid := fmt.Sprintf("%d-%s", j.n, name)
	job := &Job{id: jobid, Name: name}
	j.j[jobid] = job
	j.mutex.Unlock()
	return job
}

func (j *Jobs) Remove(id string) {
	j.mutex.Lock()
	delete(j.j, id)
	j.mutex.Unlock()
}

func (j *Jobs) Len() int {
	j.mutex.RLock()
	l := len(j.j)
	j.mutex.RUnlock()
	return l
}

func (j *Jobs) List() []*Job {
	l := make([]*Job, 0, len(j.j))
	j.mutex.RLock()
	for _, j := range j.j {
		l = append(l, j)
	}
	j.mutex.RUnlock()
	sort.Slice(l, func(i, j int) bool {
		return l[i].id < l[j].id
	})
	return l
}

type StateData struct {
	view  ui.View
	title string

	Query         string
	QueryOfResult string

	QueryOwn         string
	QueryOfOwnResult string

	Playlist string

	jobs *Jobs

	Songs    []collection.Song
	External []collection.Song
	Search   []*youtube.Result

	can map[Can]struct{}
}

func (s *StateData) View() ui.View { return s.view }

func (s *StateData) SetView(v ui.View, title string) {
	s.view = v
	s.title = title

	s.QueryOfOwnResult, s.QueryOfResult = "", ""
}

func (s *StateData) SetCan(what ...Can) {
	s.can = make(map[Can]struct{})
	for _, w := range what {
		s.can[w] = struct{}{}
	}
}

func (s *StateData) Can(what Can) bool {
	if s.can == nil {
		return false
	}
	_, ok := s.can[what]
	return ok
}

func (s *StateData) Title() string {
	title := viewNames[s.view]
	if s.title != "" {
		title = fmt.Sprintf("%s: %s", title, s.title)
	}
	return title
}

type State struct {
	sem sync.RWMutex

	s *StateData
}

func NewState() *State {
	return &State{s: &StateData{jobs: &Jobs{j: make(map[string]*Job)}}}
}

func (s *State) Do(cb func(*StateData) error) error {
	s.sem.Lock()
	err := cb(s.s)
	s.sem.Unlock()
	return err
}

type UI struct {
	ui.Output

	l      ui.ErrorReporter
	parser ui.Parser
	p      *player.Player
	c      *collection.Collection
	q      *collection.Queue

	s *State
}

func New(
	output ui.Output,
	log ui.ErrorReporter,
	parser ui.Parser,
	p *player.Player,
	c *collection.Collection,
	q *collection.Queue,
) *UI {
	return &UI{
		Output: output,
		l:      log,
		s:      NewState(),
		parser: parser,
		p:      p,
		c:      c,
		q:      q,
	}
}

func (u *UI) Input(input string) {
	cmds := u.parser.Parse(input)
	for _, cmd := range cmds {
		u.Handle(cmd)
	}
}

func (u *UI) Handle(cmd ui.Command) {
	if err := u.handle(cmd); err != nil {
		u.l.Err(err)
		return
	}

	u.Refresh()
}

func (u *UI) SetExternal(title string, ext []collection.Song) {
	u.s.Do(func(s *StateData) error {
		s.SetView(ui.ViewExternal, title)
		s.External = ext
		return nil
	})
}

func (u *UI) Refresh() {
	if err := u.refresh(); err != nil {
		u.l.Err(err)
	}
}

func (u *UI) refresh() error {
	return u.s.Do(func(s *StateData) error {
		s.SetCan()
		v := s.View()
		switch v {
		case ui.ViewHelp:
			return u.viewHelp(v, s)
		case ui.ViewSearch:
			return u.viewSearch(v, s)
		case ui.ViewSearchOwn:
			return u.viewSearchOwn(v, s)
		case ui.ViewPlaylists:
			return u.viewPlaylists(v, s)
		case ui.ViewPlaylist:
			return u.viewPlaylist(v, s)
		case ui.ViewQueue:
			return u.viewQueue(v, s)
		case ui.ViewJobs:
			return u.viewJobs(v, s)
		case ui.ViewExternal:
			return u.viewExternal(v, s)
		}

		return nil
	})
}

func (u *UI) viewHelp(view ui.View, s *StateData) error {
	n := u.help()
	u.AtomicFlush(func(a ui.AtomicOutput) {
		a.SetView(view)
		a.SetTitle(s.Title())
		a.SetText(n)
	})
	return nil
}

func (u *UI) viewSearch(view ui.View, s *StateData) error {
	s.SetCan(CanSearchResult, CanQueue)

	if s.Query != s.QueryOfResult {
		result, err := youtube.Search(s.Query)
		if err != nil {
			return err
		}
		s.Search = result
		s.QueryOfResult = s.Query
	}

	songs := make([]ui.Song, 0, len(s.Search))
	for _, s := range s.Search {
		songs = append(songs, ui.NewUISong(u.c.FromYoutube(s), false))
	}

	u.AtomicFlush(func(a ui.AtomicOutput) {
		a.SetView(view)
		a.SetTitle(s.Title())
		a.SetSongs(songs)
	})

	return nil
}

func (u *UI) viewExternal(view ui.View, s *StateData) error {
	s.SetCan(CanSong, CanQueue)
	songs := make([]ui.Song, 0, len(s.External))
	for _, s := range s.External {
		songs = append(songs, ui.NewUISong(s, false))
	}

	s.Songs = s.External
	u.AtomicFlush(func(a ui.AtomicOutput) {
		a.SetView(view)
		a.SetTitle(s.Title())
		a.SetSongs(songs)
	})

	return nil
}

func (u *UI) viewJobs(view ui.View, s *StateData) error {
	s.SetCan(CanCancelJob)
	jobs := s.jobs.List()
	l := make([]string, len(jobs))
	for i, j := range jobs {
		l[i] = fmt.Sprintf("%2d %s %3d%%", i+1, j.Name, int(100*j.Progress))
	}

	u.AtomicFlush(func(a ui.AtomicOutput) {
		a.SetView(view)
		a.SetTitle(s.Title())
		a.SetText(strings.Join(l, "\n"))
	})

	return nil
}

func (u *UI) viewSearchOwn(view ui.View, s *StateData) error {
	s.SetCan(CanSong, CanQueue)

	if s.QueryOwn != s.QueryOfOwnResult {
		result := u.c.Search(s.QueryOwn)
		s.Songs = result
		s.QueryOfOwnResult = s.QueryOwn
	}

	songs := make([]ui.Song, 0, len(s.Songs))
	for _, s := range s.Songs {
		songs = append(songs, ui.NewUISong(s, false))
	}

	u.AtomicFlush(func(a ui.AtomicOutput) {
		a.SetView(view)
		a.SetTitle(s.Title())
		a.SetSongs(songs)
	})

	return nil
}

func (u *UI) viewPlaylists(view ui.View, s *StateData) error {
	s.SetCan()

	l := u.c.List()
	sort.Strings(l)
	u.AtomicFlush(func(a ui.AtomicOutput) {
		a.SetView(view)
		a.SetTitle(s.Title())
		a.SetText(strings.Join(l, "\n"))
	})

	return nil
}

func (u *UI) viewPlaylist(view ui.View, s *StateData) error {
	s.SetCan(CanSong, CanSongRemove, CanMove, CanQueue)

	result, err := u.c.PlaylistSongs(s.Playlist)
	if err != nil {
		return err
	}

	songs := make([]ui.Song, 0, len(result))
	for _, s := range result {
		songs = append(songs, ui.NewUISong(s, false))
	}
	s.Songs = result

	u.AtomicFlush(func(a ui.AtomicOutput) {
		a.SetView(view)
		a.SetTitle(s.Title())
		a.SetSongs(songs)
	})
	return nil
}

func (u *UI) viewQueue(view ui.View, s *StateData) error {
	s.SetCan(CanSong)

	ix := u.q.CurrentIndex()
	result := u.q.Slice()
	songs := make([]ui.Song, 0, len(result))
	for i, s := range result {
		songs = append(songs, ui.NewUISong(s, ix == i))
	}
	s.Songs = result

	u.AtomicFlush(func(a ui.AtomicOutput) {
		a.SetView(view)
		a.SetTitle(s.Title())
		a.SetSongs(songs)
	})
	return nil
}

func (u *UI) help() string {
	text := make([]string, 0)
	n := u.parser.Help()
	cmdStr := func(h ui.HelpEntry) (string, string) {
		args := make([]string, 0)
		switch h.Args {
		case ui.Varadic:
			args = append(args, "...args")
		default:
			for i := 0; i < int(h.Args); i++ {
				args = append(args, fmt.Sprintf("arg%d", i+1))
			}
		}

		return strings.Join(h.Cmds, ", "), strings.Join(args, " ")
	}

	lCmd := 0
	lArg := 0
	for _, h := range n {
		cmd, arg := cmdStr(h)
		l := len(cmd)
		la := len(arg)
		if l > lCmd {
			lCmd = l
		}
		if la > lArg {
			lArg = la
		}
	}
	format := "%-" + strconv.Itoa(lCmd) + "s %-" + strconv.Itoa(lArg) + "s %s"

	for _, h := range n {
		if h.Type == ui.CmdHelp {
			continue
		}
		cmd, arg := cmdStr(h)
		if len(h.Help) == 0 {
			text = append(text, fmt.Sprintf(format, cmd, arg, ""))
			continue
		}
		text = append(text, fmt.Sprintf(format, cmd, arg, h.Help[0]))
		for _, h := range h.Help[1:] {
			text = append(text, fmt.Sprintf(format, "", "", h))
		}
	}

	return strings.Join(text, "\n")
}

func (u *UI) handle(cmd ui.Command) error {
	cmdType := cmd.Type()
	if cmdType == ui.CmdNone {
		return fmt.Errorf("%s is not a valid command", cmd.Cmd())
	}

	args := cmd.Args()
	am := cmd.ArgAmount()
	if am != ui.Varadic && int(am) != len(args) {
		n := "arguments"
		if am == 1 {
			n = "argument"
		}
		return fmt.Errorf("%s expects %d %s", cmd.Cmd(), am, n)
	}

	switch cmdType {
	case ui.CmdHelp:
		return u.handleHelp(cmd)
	case ui.CmdPlay:
		return u.handlePlay(cmd)
	case ui.CmdPause:
		return u.handlePause(cmd)
	case ui.CmdPauseToggle:
		return u.handlePauseToggle(cmd)
	case ui.CmdVolume:
		return u.handleVolume(cmd)
	case ui.CmdNext:
		return u.handleNext(cmd)
	case ui.CmdPrev:
		return u.handlePrev(cmd)
	case ui.CmdSetSongIndex:
		return u.handleSetSongIndex(cmd)
	case ui.CmdMove:
		return u.handleMove(cmd)
	case ui.CmdSearch:
		return u.handleSearch(cmd)
	case ui.CmdPlaylistAdd:
		return u.handlePlaylistAdd(cmd)
	case ui.CmdPlaylistDelete:
		return u.handlePlaylistDelete(cmd)
	case ui.CmdSongAdd:
		return u.handleSongAdd(cmd)
	case ui.CmdSongDelete:
		return u.handleSongDelete(cmd)
	case ui.CmdSeek:
		return u.handleSeek(cmd)
	case ui.CmdQueue:
		return u.handleQueue(cmd)
	case ui.CmdQueueAfter:
		return u.handleQueueAfter(cmd)
	case ui.CmdQueueClear:
		return u.handleQueueClear(cmd)
	case ui.CmdViewQueue:
		return u.handleViewQueue(cmd)
	case ui.CmdViewPlaylist:
		return u.handleViewPlaylist(cmd)
	case ui.CmdViewPlaylists:
		return u.handleViewPlaylists(cmd)
	case ui.CmdSearchOwn:
		return u.handleSearchOwn(cmd)
	case ui.CmdScrape:
		return u.handleScrape(cmd)
	case ui.CmdJobs:
		return u.handleJobs(cmd)
	case ui.CmdCancelJob:
		return u.handleCancelJobs(cmd)
	default:
		return fmt.Errorf("%s is not implemented", cmd.Cmd())
	}
}

func (u *UI) handleHelp(cmd ui.Command) error {
	return u.s.Do(func(s *StateData) error {
		s.SetView(ui.ViewHelp, "")
		return nil
	})
}

func (u *UI) handlePlay(cmd ui.Command) error  { u.p.Play(); return nil }
func (u *UI) handlePause(cmd ui.Command) error { u.p.Pause(); return nil }
func (u *UI) handleNext(cmd ui.Command) error  { u.p.Next(); return nil }
func (u *UI) handlePrev(cmd ui.Command) error  { u.p.Prev(); return nil }

func (u *UI) handleVolume(cmd ui.Command) error {
	n, ok := cmd.Args()[0].Int()
	if !ok {
		return fmt.Errorf("%s requires an integer argument", cmd.Cmd())
	}
	u.p.IncreaseVolume(float64(n) / 100)
	return nil
}

func (u *UI) handlePauseToggle(cmd ui.Command) error {
	if u.p.Paused() {
		u.p.Play()
		return nil
	}
	u.p.Pause()
	return nil
}

func (u *UI) handleSetSongIndex(cmd ui.Command) error {
	ix, ok := cmd.Args()[0].Int()
	if !ok {
		return fmt.Errorf("%s requires an integer argument, i.e.: index in queue", cmd.Cmd())
	}

	u.q.SetCurrentIndex(ix - 1)
	u.p.ForcePlay()

	return nil
}

func (u *UI) handleMove(cmd ui.Command) error {
	args := cmd.Args()
	f, ok := args[0].IntRange()
	if !ok {
		return fmt.Errorf("%s requires arg1 to be an integer", cmd.Cmd())
	}
	t, ok := args[1].Int()
	if !ok {
		return fmt.Errorf("%s requires arg2 to be an integer", cmd.Cmd())
	}

	t--
	for i := range f {
		f[i]--
	}

	return u.s.Do(func(s *StateData) error {
		if !s.Can(CanMove) {
			return fmt.Errorf("%s can only be used inside of a playlist", cmd.Cmd())
		}

		return u.c.MoveSongIndex(s.Playlist, f, t)
	})
}

func (u *UI) handlePlaylistAdd(cmd ui.Command) error {
	s := cmd.Args()[0].String()
	_, err := strconv.Atoi(s)
	if err == nil {
		return fmt.Errorf("playlist name mustn't be a number")
	}

	return u.c.Create(cmd.Args()[0].String())
}

func (u *UI) handlePlaylistDelete(cmd ui.Command) error {
	return u.c.Delete(cmd.Args()[0].String())
}

func (u *UI) handleSongAdd(cmd ui.Command) error {
	args := cmd.Args()
	p := args[0].String()

	add := func(songs []collection.Song) error {
		for _, s := range songs {
			if err := u.c.AddSong(p, s); err != nil {
				return err
			}
		}
		return nil
	}

	ints, ok := args[1].IntRange()
	if ok {
		return u.s.Do(func(s *StateData) error {
			songs, err := u.fromResults(ints, s)
			if err != nil {
				songs, err = u.fromSongs(ints, s)
			}
			if err != nil {
				return err
			}
			return add(songs)
		})
	}

	songs, err := u.fromURLs(args[1:].Strings())
	if err := add(songs); err != nil {
		return err
	}
	return err
}

func (u *UI) handleSongDelete(cmd ui.Command) error {
	args := cmd.Args()
	ints, ok := args[0].IntRange()
	if !ok {
		return fmt.Errorf("%s requires arg2 to be an int or int range", cmd.Cmd())
	}
	for i := range ints {
		ints[i]--
	}

	return u.s.Do(func(s *StateData) error {
		if !s.Can(CanSongRemove) {
			return fmt.Errorf("%s can only be done when viewing a playlist", cmd.Cmd())
		}

		if err := u.c.DelSongIndexes(s.Playlist, ints); err != nil {
			return err
		}

		return nil
	})
}

func (u *UI) handleSeek(cmd ui.Command) error {
	n := cmd.Args()[0].String()
	generic := fmt.Errorf("%s requires arg1 to be an integer or duration", cmd.Cmd())
	if len(n) == 0 {
		return generic
	}

	sign := 1
	if n[0] == '-' {
		sign = -1
	}
	relative := n[0] == '+' || n[0] == '-'
	if relative {
		n = n[1:]
	}

	var h, m, s int
	err := func() error {
		if _, err := fmt.Sscanf(n, "%d:%d:%d", &h, &m, &s); err == nil {
			return nil
		}
		h, m, s = 0, 0, 0
		if _, err := fmt.Sscanf(n, "%d:%d", &m, &s); err == nil {
			return nil
		}
		h, m, s = 0, 0, 0
		if _, err := fmt.Sscanf(n, "%d", &s); err == nil {
			return nil
		}
		return generic
	}()
	if err != nil {
		return err
	}

	s += m*60 + h*3600
	s *= sign
	whence := io.SeekStart
	if relative {
		whence = io.SeekCurrent
	}

	u.p.Seek(time.Second*time.Duration(s), whence)
	return nil
}

func (u *UI) handleViewPlaylist(cmd ui.Command) error {
	pl := cmd.Args()[0].String()
	return u.s.Do(func(s *StateData) error {
		s.SetView(ui.ViewPlaylist, pl)
		s.Playlist = pl
		return nil
	})
}

func (u *UI) handleViewPlaylists(cmd ui.Command) error {
	return u.s.Do(func(s *StateData) error {
		s.SetView(ui.ViewPlaylists, "")
		return nil
	})
}

func (u *UI) handleViewQueue(cmd ui.Command) error {
	return u.s.Do(func(s *StateData) error {
		s.SetView(ui.ViewQueue, "")
		return nil
	})
}

func (u *UI) handleSearch(cmd ui.Command) error {
	q := cmd.Args().String()
	if q == "" {
		return fmt.Errorf("%s requires a search query parameter", cmd.Cmd())
	}

	return u.s.Do(func(s *StateData) error {
		s.SetView(ui.ViewSearch, q)
		s.Query = q
		return nil
	})
}

func (u *UI) handleSearchOwn(cmd ui.Command) error {
	q := cmd.Args().String()
	if q == "" {
		return fmt.Errorf("%s requires a search query parameter", cmd.Cmd())
	}

	return u.s.Do(func(s *StateData) error {
		s.SetView(ui.ViewSearchOwn, q)
		s.QueryOwn = q
		return nil
	})
}

func (u *UI) handleJobs(cmd ui.Command) error {
	return u.s.Do(func(s *StateData) error {
		s.SetView(ui.ViewJobs, "")
		return nil
	})
}

func (u *UI) handleCancelJobs(cmd ui.Command) error {
	ints, ok := cmd.Args()[0].IntRange()
	if !ok {
		return fmt.Errorf("%s requires a range of jobs to cancel", cmd.Cmd())
	}

	return u.s.Do(func(s *StateData) error {
		jobs := s.jobs.List()
		cancel := make([]*Job, 0)
		for _, n := range ints {
			n--
			if n < 0 || n >= len(jobs) {
				return fmt.Errorf("invalid range")
			}
			cancel = append(cancel, jobs[n])
		}

		for _, c := range cancel {
			c.Cancel()
		}

		return nil
	})
}

func (u *UI) handleScrape(cmd ui.Command) error {
	args := cmd.Args()
	if len(args) < 2 {
		return fmt.Errorf("%s requires at least a playlist name and a url", cmd.Cmd())
	}

	pl := args[0].String()
	if !u.c.Exists(pl) {
		return fmt.Errorf("%s: playlist %s does not exist", cmd.Cmd(), pl)
	}

	_depth := args[len(args)-1]
	args = args[1 : len(args)-1]

	depth, ok := _depth.Int()
	if !ok {
		depth = 1
		args = args[:len(args)+1]
	}
	if depth < 0 {
		depth = 0
	}

	uris := args.Strings()
	if len(uris) == 0 {
		return fmt.Errorf("%s requires at least one url", cmd.Cmd())
	}

	concurrency := 32
	totalJobs := len(uris)
	u.s.Do(func(s *StateData) error {
		totalJobs += s.jobs.Len()
		return nil
	})

	concurrency /= totalJobs
	if concurrency < 1 {
		concurrency = 1
	}

	for _, uri := range uris {
		var job *Job
		u.s.Do(func(s *StateData) error {
			job = s.jobs.Add(fmt.Sprintf("scrape: %s %s", pl, uri))
			s.SetView(ui.ViewJobs, "")
			return nil
		})

		go func(uri string) {
			defer u.s.Do(func(s *StateData) error {
				s.jobs.Remove(job.ID())
				return nil
			})

			scr := scraper.New(scraper.Config{
				Concurrency: concurrency,
				MaxDepth:    depth,
				Callback: func(uri *url.URL, doc *goquery.Document, depth, item, total int) error {
					if total == 0 {
						return nil
					}
					job.Progress = float64(item) / float64(total)
					return nil
				},
			})

			ctx, cancel := context.WithCancel(context.Background())
			job.SetCancel(cancel)

			err := youtube.NewScraper(scr, func(r *youtube.Result) {
				if err := u.c.AddSong(pl, u.c.FromYoutube(r)); err != nil {
					u.l.Err(fmt.Errorf("%s error: %w", cmd.Cmd(), err))
				}
			}).ScrapeWithContext(ctx, uri)
			if err != nil {
				u.l.Err(fmt.Errorf("%s error: %w", cmd.Cmd(), err))
				return
			}
		}(uri)
	}

	return nil
}

func (u *UI) handleQueueClear(cmd ui.Command) error {
	u.q.Reset()
	u.p.ForcePlay()
	return nil
}

func (u *UI) queue(cmd string, arg ui.Arg, ix int) error {
	str := arg.String()
	y, err := u.c.FromYoutubeURL(str, "")
	if err == nil {
		u.c.QueueSong(ix, y)
		return nil
	}

	if err := u.c.Queue(ix, str); err == nil {
		return nil
	}

	ints, ok := arg.IntRange()
	if !ok {
		return fmt.Errorf("%s requires a range of songs", cmd)
	}

	return u.s.Do(func(s *StateData) error {
		songs, err := u.fromResults(ints, s)
		if err != nil {
			songs, err = u.fromSongs(ints, s)
		}
		if err != nil {
			return err
		}

		for _, s := range songs {
			u.c.QueueSong(ix, s)
			if ix >= 0 {
				ix++
			}
		}

		return nil
	})
}

func (u *UI) handleQueue(cmd ui.Command) error {
	args := cmd.Args()
	return u.queue(cmd.Cmd(), args[0], -1)
}

func (u *UI) handleQueueAfter(cmd ui.Command) error {
	args := cmd.Args()

	if args[0].String() == "next" {
		ix := u.q.CurrentIndex()
		return u.queue(cmd.Cmd(), args[1], ix+2)
	}

	ix, ok := args[0].Int()
	if !ok || ix < 0 {
		return fmt.Errorf("%s requires arg 1 to be an index in the queue", cmd.Cmd())
	}

	return u.queue(cmd.Cmd(), args[1], ix+1)
}

func (u *UI) fromResults(ints []int, s *StateData) ([]collection.Song, error) {
	if !s.Can(CanSearchResult) {
		return nil, errors.New("can only be used from a search result view")
	}

	songs := make([]collection.Song, 0, len(ints))
	for _, i := range ints {
		i--
		if i < 0 || i >= len(s.Search) {
			return nil, fmt.Errorf("invalid index given: %d", i)
		}
		songs = append(songs, u.c.FromYoutube(s.Search[i]))
	}

	return songs, nil
}

func (u *UI) fromSongs(ints []int, s *StateData) ([]collection.Song, error) {
	if !s.Can(CanSong) {
		return nil, errors.New("can only be used from a song view")
	}

	songs := make([]collection.Song, 0, len(ints))
	for _, i := range ints {
		i--
		if i < 0 || i >= len(s.Songs) {
			return nil, fmt.Errorf("invalid index given: %d", i)
		}
		songs = append(songs, s.Songs[i])
	}

	return songs, nil
}

func (u *UI) fromURLs(urls []string) ([]collection.Song, error) {
	var gerr error
	songs := make([]collection.Song, 0, len(urls))
	for _, url := range urls {
		s, err := u.c.FromYoutubeURL(url, "")
		if err != nil {
			gerr = err
			continue
		}
		songs = append(songs, s)
	}

	return songs, gerr
}
