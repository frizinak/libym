package di

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	libmpv "github.com/frizinak/libym/backend/mpv/lib"
	rpcmpv "github.com/frizinak/libym/backend/mpv/rpc"
	"github.com/frizinak/libym/collection"
	"github.com/frizinak/libym/player"
	"github.com/frizinak/libym/ui"
	"github.com/frizinak/libym/ui/base"
)

type Config struct {
	// Defaults to an stderr logger
	Log *log.Logger

	// Defaults to 8
	ConcurrentDownloads int

	// Defaults to ~/.cache/ym
	StorePath string

	// Defaults to os.Stderr
	BackendLogger io.Writer

	AutoSave bool

	// Mutually exclusive with CustomOutput.
	SimpleOutput io.Writer

	// Mutually exclusive with SimpleOutput.
	CustomOutput ui.Output

	CustomError ui.ErrorReporter
}

type Backend interface {
	Init() error

	player.Backend
}

type BackendBuilder struct {
	Name  string
	Build func(di *DI, log *log.Logger) (Backend, error)
}

type DI struct {
	c        Config
	backends []BackendBuilder

	store            string
	log              *log.Logger
	backend          Backend
	backendName      string
	backendAvailable error
	player           *player.Player
	queue            *collection.Queue
	collection       *collection.Collection
	baseUI           *base.UI
	commandParser    *ui.CommandParser
}

func New(c Config) *DI {
	di := &DI{c: c}
	di.backends = []BackendBuilder{
		{
			Name: "libmpv",
			Build: func(di *DI, log *log.Logger) (Backend, error) {
				return libmpv.New(log), nil
			},
		},
		{
			Name: "mpv",
			Build: func(di *DI, log *log.Logger) (Backend, error) {
				return rpcmpv.New(log, filepath.Join(di.Store(), "mpv-ipc.sock")), nil
			},
		},
	}

	return di
}

func (di *DI) BaseUI() ui.UI {
	if di.baseUI == nil {
		var s *base.SimpleOutput

		output := di.c.CustomOutput
		err := di.c.CustomError

		w := di.c.SimpleOutput
		if w == nil {
			w = os.Stdout
		}

		if output == nil {
			s = base.NewSimpleOutput(w)
			output = s
		}

		if err == nil {
			err = s
		}

		if err == nil {
			err = ui.NewLogErrorReporter(log.New(w, "UI ERR: ", 0))
		}

		di.baseUI = base.New(
			output,
			err,
			di.CommandParser(),
			di.Player(),
			di.Collection(),
			di.Queue(),
		)
	}

	return di.baseUI
}

func (di *DI) CommandParser() *ui.CommandParser {
	if di.commandParser == nil {
		di.commandParser = ui.NewParser()
		di.commandParser.Alias(ui.CmdHelp, ui.Zero, "h", "help")

		di.commandParser.Alias(ui.CmdPauseToggle, ui.Zero, "p", "pause")

		di.commandParser.Alias(ui.CmdSetSongIndex, ui.One, "p", "play", "goto")
		di.commandParser.Alias(ui.CmdNext, ui.Zero, ">", "next", "skip")
		di.commandParser.Alias(ui.CmdPrev, ui.Zero, "<", "prev", "previous")
		di.commandParser.Alias(ui.CmdSeek, ui.One, "seek")

		di.commandParser.Alias(ui.CmdPlaylistAdd, ui.One, "create-playlist")
		di.commandParser.Alias(ui.CmdPlaylistDelete, ui.One, "remove-playlist")
		di.commandParser.Alias(ui.CmdSongAdd, ui.Two, "a", "add")
		di.commandParser.Alias(ui.CmdSongDelete, ui.One, "del", "delete")

		di.commandParser.Alias(ui.CmdVolume, ui.One, "v", "volume")

		di.commandParser.Alias(ui.CmdSearch, ui.Varadic, "s", "search")
		di.commandParser.Alias(ui.CmdSearchOwn, ui.Varadic, "/", "find")

		di.commandParser.Alias(ui.CmdQueueClear, ui.Zero, "clear")
		di.commandParser.Alias(ui.CmdQueue, ui.One, "q", "queue")
		di.commandParser.Alias(ui.CmdViewQueue, ui.Zero, "q", "queue")

		di.commandParser.Alias(ui.CmdMove, ui.Two, "mv", "move")

		di.commandParser.Alias(ui.CmdViewPlaylist, ui.One, "ls", "playlist")
		di.commandParser.Alias(ui.CmdViewPlaylists, 0, "ls", "playlists")
	}

	return di.commandParser
}

func (di *DI) Log() *log.Logger {
	if di.log == nil {
		di.log = di.c.Log
		if di.log == nil {
			di.log = log.New(os.Stderr, "", 0)
		}
	}

	return di.log
}

func (di *DI) Store() string {
	if di.store == "" {
		if di.c.StorePath != "" {
			di.store = di.c.StorePath
			return di.store
		}

		cache, err := os.UserCacheDir()
		if err != nil {
			panic(err)
		}
		di.store = filepath.Join(cache, "ym")
	}

	return di.store
}

func (di *DI) BackendAvailable() (string, error) {
	di.Backend()
	return di.backendName, di.backendAvailable
}

func (di *DI) Backend() Backend {
	if di.backend == nil {
		w := di.c.BackendLogger
		if w == nil {
			w = os.Stderr
		}
		for _, b := range di.backends {
			di.backendName = b.Name

			l := log.New(w, strings.ToUpper(b.Name)+": ", 0)
			be, err := b.Build(di, l)
			if err != nil {
				l.Println(err)
				di.backendAvailable = err
				continue
			}

			if err := be.Init(); err != nil {
				l.Println(err)
				di.backendAvailable = err
				continue
			}

			di.backend = be
			di.backendAvailable = nil
			break
		}
	}

	return di.backend
}

func (di *DI) Queue() *collection.Queue {
	if di.queue == nil {
		di.queue = collection.NewQueue()
	}
	return di.queue
}

func (di *DI) Player() *player.Player {
	if di.player == nil {
		di.player = player.NewPlayer(di.Backend(), di.Queue())
	}
	return di.player
}

func (di *DI) Collection() *collection.Collection {
	if di.collection == nil {
		l := di.Log()
		n := di.c.ConcurrentDownloads
		if n <= 0 {
			n = 8
		}
		di.collection = collection.New(l, di.Store(), di.Queue(), n, di.c.AutoSave)
		if err := di.collection.Init(); err != nil {
			panic(err)
		}
	}
	return di.collection
}
