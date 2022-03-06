//go:build cgo
// +build cgo

package lib

import (
	"log"

	"github.com/YouROK/go-mpv/mpv"
	wrap "github.com/frizinak/libym/backend/mpv"
)

func New(log *log.Logger) *wrap.MPV {
	return wrap.New(log, &LibMPV{})
}

type LibMPV struct {
	mpv     *mpv.Mpv
	closing bool
}

func (m *LibMPV) Init(events chan<- wrap.Event) error {
	m.mpv = mpv.Create()

	if err := m.mpv.SetOption("vid", mpv.FORMAT_FLAG, false); err != nil {
		return err
	}
	_ = m.mpv.SetOption("really-quiet", mpv.FORMAT_FLAG, true)

	if err := m.mpv.Initialize(); err != nil {
		return err
	}

	ev := map[mpv.EventId]wrap.EventID{
		mpv.EVENT_END_FILE:   wrap.EventEndFile,
		mpv.EVENT_START_FILE: wrap.EventStartFile,
		mpv.EVENT_PAUSE:      wrap.EventPause,
		mpv.EVENT_UNPAUSE:    wrap.EventUnpause,
	}

	go func() {
		for {
			if m.closing {
				break
			}
			e := m.mpv.WaitEvent(1)
			if id, ok := ev[e.Event_Id]; ok {
				events <- wrap.Event{id}
			}
		}
	}()

	return nil
}

func (m *LibMPV) Close() error {
	if m.closing {
		return nil
	}
	m.closing = true
	m.mpv.TerminateDestroy()
	return nil
}

func (m *LibMPV) GetPropertyDouble(n string) (float64, error) {
	v, err := m.mpv.GetProperty(n, mpv.FORMAT_DOUBLE)
	if err != nil || v == nil {
		return 0, err
	}
	return v.(float64), err
}

func (m *LibMPV) SetPropertyDouble(n string, v float64) error {
	return m.mpv.SetProperty(n, mpv.FORMAT_DOUBLE, v)
}

func (m *LibMPV) SetPropertyString(n, v string) error {
	return m.mpv.SetPropertyString(n, v)
}

func (m *LibMPV) SetPropertyBool(n string, v bool) error {
	sv := "no"
	if v {
		sv = "yes"
	}
	return m.SetPropertyString(n, sv)
}

func (m *LibMPV) Command(cmd ...string) error { return m.mpv.Command(cmd) }
