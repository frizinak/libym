package ui

import (
	"fmt"
	"strconv"
	"strings"
)

type UI interface {
	Input(string)
	//Handle(Command)
	Refresh()
}

type Parser interface {
	Parse(string) []Command
	Help() Help
}

type BaseSong interface {
	Title() string
}

type Song interface {
	BaseSong
	Active() bool
}

type SimpleUISong struct {
	BaseSong
	active bool
}

func NewUISong(s BaseSong, a bool) Song { return SimpleUISong{s, a} }
func (s SimpleUISong) Active() bool     { return s.active }

type Output interface {
	SetTitle(string)
	SetSongs([]Song)
	SetText(string)
	AtomicFlush(func())
	Flush()
}

type ErrorReporter interface {
	Err(error)
	Errf(string, ...interface{})
}

type Printlner interface {
	Println(...interface{})
	Printf(string, ...interface{})
}

type LogErrorReporter struct {
	Printlner
}

func (l *LogErrorReporter) Err(err error)                        { l.Println("ERR", err) }
func (l *LogErrorReporter) Errf(format string, v ...interface{}) { l.Printf(format, v...) }

func NewLogErrorReporter(l Printlner) ErrorReporter {
	return &LogErrorReporter{l}
}

type CommandType int

const (
	CmdNone CommandType = iota
	CmdHelp
	CmdPlay
	CmdPause
	CmdPauseToggle
	CmdNext
	CmdPrev
	CmdSetSongIndex
	CmdMove
	CmdSearch
	CmdPlaylistAdd
	CmdPlaylistDelete
	CmdSongAdd
	CmdSongDelete
	CmdSeek
	CmdQueue
	CmdQueueClear
	CmdViewQueue
	CmdViewPlaylist
	CmdViewPlaylists
	CmdSearchOwn
	CmdVolume
)

type ArgAmount byte

const (
	Zero    ArgAmount = 0
	One     ArgAmount = 1
	Two     ArgAmount = 2
	Varadic ArgAmount = 255
)

var texts = map[CommandType]string{
	CmdNone:           "",
	CmdPlay:           "play",
	CmdPause:          "pause",
	CmdPauseToggle:    "toggle pause",
	CmdNext:           "next song in queue",
	CmdPrev:           "previous song in queue",
	CmdSetSongIndex:   "play a specific song by index in the queue",
	CmdMove:           "move a song in a playlist",
	CmdSearch:         "search for a song",
	CmdPlaylistAdd:    "add a new playlist",
	CmdPlaylistDelete: "delete a playlist",
	CmdSongAdd:        "add a song to a playlist",
	CmdSongDelete:     "delete a song from a playlist",
	CmdSeek:           "seek in the current song (relative: +n | -n, absolute: n, where n is h:m:s, m:s or s)",
	CmdQueue:          "queue a song from a playlist or a search result",
	CmdQueueClear:     "clear queue",
	CmdViewQueue:      "switch to queue view",
	CmdViewPlaylist:   "switch to a playlist view",
	CmdViewPlaylists:  "list all playlists",
	CmdSearchOwn:      "search for songs across playlists",
}

type Args []Arg

func (a Args) String() string {
	return strings.Join(a.Strings(), " ")
}

func (a Args) Strings() []string {
	l := make([]string, len(a))
	for i, n := range a {
		l[i] = string(n)
	}
	return l
}

func (a Args) Ints() ([]int, bool) {
	l := make([]int, 0, len(a))
	allOK := true
	for _, n := range a {
		ints, ok := n.IntRange()
		if !ok {
			allOK = false
		}
		l = append(l, ints...)
	}

	return l, allOK
}

type Arg string

func (a Arg) IntRange() ([]int, bool) {
	comma := strings.Split(string(a), ",")
	r := make([]int, 0, len(comma))
	for _, n := range comma {
		dash := strings.SplitN(n, "-", 2)
		v, err := strconv.Atoi(strings.TrimSpace(dash[0]))
		if err != nil {
			return r, false
		}
		if len(dash) != 2 {
			r = append(r, v)
			continue
		}

		if len(dash) == 2 {
			v2, err := strconv.Atoi(strings.TrimSpace(dash[1]))
			if err != nil {
				return r, false
			}
			if v2 < v {
				return r, false
			}
			for i := v; i <= v2; i++ {
				r = append(r, i)
			}
		}
	}

	return r, len(r) > 0
}

func (a Arg) Int() (int, bool) {
	n, err := strconv.Atoi(string(a))
	return n, err == nil
}

func (a Arg) String() string { return string(a) }

type Command struct {
	t       CommandType
	a       Args
	aAmount ArgAmount
	cmd     string
}

func (c Command) Type() CommandType    { return c.t }
func (c Command) Args() Args           { return c.a }
func (c Command) ArgAmount() ArgAmount { return c.aAmount }
func (c Command) Cmd() string          { return c.cmd }

type Help []HelpEntry

type HelpEntry struct {
	Type CommandType
	Args ArgAmount
	Cmds []string
	Text string
}

type CommandParser struct {
	alias map[string]map[ArgAmount]CommandType
	help  Help
}

func NewParser() *CommandParser {
	return &CommandParser{
		make(map[string]map[ArgAmount]CommandType),
		make(Help, 0),
	}
}

func (c *CommandParser) Parse(input string) []Command {
	n := strings.Split(input, ";")
	cmds := make([]Command, 0, len(n))
	for _, str := range n {
		str = strings.TrimSpace(str)
		if str == "" {
			continue
		}
		cmds = append(cmds, c.parse(str))
	}

	return cmds
}

func (c *CommandParser) Alias(t CommandType, a ArgAmount, command ...string) {
	for _, cmd := range command {
		if _, ok := c.alias[cmd]; !ok {
			c.alias[cmd] = make(map[ArgAmount]CommandType)
		}
		if _, ok := c.alias[cmd][a]; ok {
			panic(fmt.Sprintf("an alias already exists for %s:%d", cmd, a))
		}
		c.alias[cmd][a] = t
	}

	c.help = append(c.help, HelpEntry{t, a, command, texts[t]})
}

func (c *CommandParser) Help() Help {
	return c.help
}

func (c *CommandParser) parse(input string) (cmd Command) {
	t := c.tokens(input)
	if len(t) == 0 {
		return
	}
	cmd.cmd = t[0]
	cmd.a = make(Args, len(t)-1)
	for i, a := range t[1:] {
		cmd.a[i] = Arg(a)
	}

	if _, ok := c.alias[cmd.cmd]; !ok {
		return
	}

	list := c.alias[cmd.cmd]
	testAmount := ArgAmount(len(cmd.a))
	if r, ok := list[testAmount]; ok {
		cmd.t = r
		cmd.aAmount = testAmount
		return
	}

	if r, ok := list[Varadic]; ok {
		cmd.t = r
		cmd.aAmount = testAmount
		return
	}

	for i := range list {
		cmd.t = list[i]
		cmd.aAmount = i
	}

	return
}

func (c *CommandParser) tokens(input string) []string {
	n := strings.Split(input, " ")
	m := make([]string, 0, len(n))
	for _, s := range n {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}

		m = append(m, s)
	}

	return m
}
