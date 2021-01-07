package mpv

import (
	"log"
	"sync"
	"time"

	"github.com/YouROK/go-mpv/mpv"
)

func New(log *log.Logger) *LibMPV {
	return &LibMPV{log: log}
}

type LibMPV struct {
	log *log.Logger

	sem sync.Mutex

	state struct {
		volume float64
		dones  []chan struct{}
		starts chan chan struct{}

		paused bool
	}

	mpv *mpv.Mpv
}

func (m *LibMPV) l(err error, debug string) {
	if err != nil {
		m.log.Println(debug, err)
	}
}

func (m *LibMPV) Init() error {
	m.mpv = mpv.Create()
	m.state.dones = make([]chan struct{}, 0)
	m.state.starts = make(chan chan struct{})
	m.state.paused = true

	m.l(m.mpv.SetOption("vid", mpv.FORMAT_FLAG, false), "vid")
	m.l(m.mpv.SetOption("really-quiet", mpv.FORMAT_FLAG, true), "really quiet")

	if err := m.mpv.Initialize(); err != nil {
		return err
	}

	_vol, err := m.mpv.GetProperty("volume", mpv.FORMAT_DOUBLE)
	m.l(err, "volume")
	m.state.volume = _vol.(float64) / 100

	actualPause := false
	go func() {
		for {
			e := m.mpv.WaitEvent(1)
			switch e.Event_Id {
			case mpv.EVENT_END_FILE:
				m.state.dones[0] <- struct{}{}
				m.state.dones = m.state.dones[1:]
			case mpv.EVENT_START_FILE:
				if !actualPause {
					m.state.paused = false
				}
				done := make(chan struct{}, 1)
				m.state.dones = append(m.state.dones, done)
				m.state.starts <- done
			case mpv.EVENT_PAUSE:
				actualPause = true
				m.state.paused = true
			case mpv.EVENT_UNPAUSE:
				actualPause = false
				m.state.paused = false
			}
		}
	}()

	return nil
}

func (m *LibMPV) Stop() {
	m.l(m.mpv.Command([]string{"stop"}), "stop")
}

func (m *LibMPV) Play(file string) (chan struct{}, error) {
	m.sem.Lock()
	defer m.sem.Unlock()
	err := m.mpv.Command([]string{"loadfile", file, "replace"})
	if err != nil {
		return nil, err
	}
	done := <-m.state.starts
	return done, nil
}

func (m *LibMPV) Paused() bool { return m.state.paused }

func (m *LibMPV) Pause(pause bool) {
	p := "no"
	if pause {
		p = "yes"
	}
	m.l(m.mpv.SetPropertyString("pause", p), "pause")
}

func (m *LibMPV) TogglePause() {
	n := true
	if m.state.paused {
		n = false
	}
	m.Pause(n)
}

func (m *LibMPV) SetVolume(n float64) {
	if n < 0 {
		n = 0
	}
	if n > 1 {
		n = 1
	}
	if err := m.mpv.SetProperty("volume", mpv.FORMAT_DOUBLE, n*100); err != nil {
		m.l(err, "volume")
		return
	}

	m.state.volume = n
}

func (m *LibMPV) IncreaseVolume(n float64) {
	v := m.state.volume
	v += n
	m.SetVolume(v)
}

func (m *LibMPV) Volume() float64 { return m.state.volume }

func (m *LibMPV) Seek(adjustment time.Duration) {
	if adjustment == 0 {
		return
	}
	_cur, err := m.mpv.GetProperty("time-pos", mpv.FORMAT_INT64)
	if err != nil {
		m.l(err, "time-pos")
		return
	}

	cur := _cur.(int64) + int64(adjustment.Seconds())
	if cur < 0 {
		cur = 0
	}

	m.l(m.mpv.SetProperty("time-pos", mpv.FORMAT_INT64, cur), "time-pos2")
}

func (m *LibMPV) SeekTo(to float64) {
	m.l(m.mpv.SetProperty("percent-pos", mpv.FORMAT_DOUBLE, to), "percent-pos")
}

func (m *LibMPV) Position() float64 {
	_v, err := m.mpv.GetProperty("percent-pos", mpv.FORMAT_DOUBLE)
	if err != nil {
		return 0
	}

	return _v.(float64) / 100
}

func (m *LibMPV) Duration() time.Duration {
	_v, err := m.mpv.GetProperty("duration", mpv.FORMAT_DOUBLE)
	if err != nil {
		return 0
	}
	return time.Duration(_v.(float64)) * time.Second
}
