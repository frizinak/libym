package di

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

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

	// Ratelimit ratelimits youtube.com calls by pulling items for the given
	// channels before each action.
	// If nil, a default ratelimiter of 1 item every 5 seconds is used for each.
	RatelimitDownloads <-chan struct{}
	RatelimitMeta      <-chan struct{}
}

// MakeRateLimit creates and starts a ratelimiter that can be used in Config.
func MakeRatelimit(amount int, interval time.Duration) <-chan struct{} {
	if amount < 1 {
		amount = 1
	}
	ch := make(chan struct{}, amount)
	go func() {
		for {
			for i := 0; i < amount; i++ {
				ch <- struct{}{}
			}
			time.Sleep(interval)
		}
	}()

	return ch
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
	rlDownload       <-chan struct{}
	rlMeta           <-chan struct{}
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

func (di *DI) Rates() (<-chan struct{}, <-chan struct{}) {
	if di.rlDownload == nil {
		di.rlDownload = di.c.RatelimitDownloads
		if di.rlDownload == nil {
			di.rlDownload = MakeRatelimit(1, time.Second*5)
		}
	}
	if di.rlMeta == nil {
		di.rlMeta = di.c.RatelimitMeta
		if di.rlMeta == nil {
			di.rlMeta = MakeRatelimit(1, time.Second*5)
		}
	}

	return di.rlDownload, di.rlMeta
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

		col := di.Collection()

		dl, meta := di.Rates()
		col.Run(dl, meta)

		di.baseUI = base.New(
			output,
			err,
			di.CommandParser(),
			di.Player(),
			col,
			di.Queue(),
		)
	}

	return di.baseUI
}

func (di *DI) CommandParser() *ui.CommandParser {
	if di.commandParser == nil {
		di.commandParser = ui.NewParser()
		di.commandParser.Alias(ui.CmdHelp, ui.Zero, nil, "h", "help")

		di.commandParser.Alias(ui.CmdPauseToggle, ui.Zero, nil, "p", "pause")

		di.commandParser.Alias(ui.CmdSetSongIndex, ui.One, []string{"e.g.: p 10"}, "p", "play", "goto")
		di.commandParser.Alias(ui.CmdNext, ui.Zero, nil, ">", "next", "skip")
		di.commandParser.Alias(ui.CmdPrev, ui.Zero, nil, "<", "prev", "previous")
		di.commandParser.Alias(
			ui.CmdSeek,
			ui.One,
			[]string{
				"relative: +n | -n, absolute: n, where n is h:m:s, m:s or s",
				"e.g.: seek 30:00 => seek to minute 30",
				"e.g.: seek +05:00 => seek 5 minutes further",
				"e.g.: seek -30 => seek 30 seconds back",
			},
			"seek",
		)

		di.commandParser.Alias(
			ui.CmdScrape,
			ui.Varadic,
			[]string{
				"scrape <playlist> <url...> <depth:1>",
				"e.g.: scrape hnbb https://www.hotnewbeebop.com/articles/reviews 2",
				"e.g.: scrape hnbb https://www.shmootube.com https://yougoob.com",
			},
			"scrape",
		)

		di.commandParser.Alias(ui.CmdJobs, ui.Zero, nil, "jobs")
		di.commandParser.Alias(ui.CmdCancelJob, ui.One, nil, "cancel")

		di.commandParser.Alias(ui.CmdPlaylistAdd, ui.One, nil, "create-playlist")
		di.commandParser.Alias(ui.CmdPlaylistDelete, ui.One, nil, "remove-playlist")
		di.commandParser.Alias(
			ui.CmdSongAdd,
			ui.Two,
			[]string{"e.g.: add hnbb 5,30-42"},
			"a",
			"add",
		)
		di.commandParser.Alias(ui.CmdSongDelete, ui.One, []string{"see add"}, "del", "delete")

		di.commandParser.Alias(ui.CmdVolume, ui.One, nil, "v", "volume")

		di.commandParser.Alias(ui.CmdSearch, ui.Varadic, nil, "s", "search")
		di.commandParser.Alias(ui.CmdSearchOwn, ui.Varadic, nil, "/", "find")

		di.commandParser.Alias(ui.CmdQueueClear, ui.Zero, nil, "clear")
		di.commandParser.Alias(ui.CmdQueue, ui.One, []string{"see add"}, "q", "queue")
		di.commandParser.Alias(
			ui.CmdQueueAfter,
			ui.Two,
			[]string{
				"e.g.: q 5 6,8-10 => queue songs 6,8,9,10 after song at index 5",
				"as a special case 'next' can be used to insert songs after the current song",
				"e.g.: add next 6,8-10",
			},
			"q",
			"queue",
		)
		di.commandParser.Alias(ui.CmdViewQueue, ui.Zero, nil, "q", "queue")

		di.commandParser.Alias(ui.CmdMove, ui.Two, nil, "mv", "move")

		di.commandParser.Alias(ui.CmdViewPlaylist, ui.One, nil, "ls", "playlist")
		di.commandParser.Alias(ui.CmdViewPlaylists, ui.Zero, nil, "ls", "playlists")
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
		err := di.c.CustomError
		w := di.c.SimpleOutput
		if w == nil {
			w = os.Stdout
		}
		if err == nil {
			err = ui.NewLogErrorReporter(log.New(w, "PLAYER ERR: ", 0))
		}

		store := filepath.Join(di.Store(), "player-position")
		di.player = player.NewPlayer(di.Backend(), err, di.Queue(), store)
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
