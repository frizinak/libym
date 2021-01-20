package player

import (
	"errors"
	"time"
)

var ErrNotSupported = errors.New("backend is not available, you will need to compile from source")

type UnsupportedBackend struct {
}

func (u UnsupportedBackend) Init() error                        { return ErrNotSupported }
func (u UnsupportedBackend) Close() error                       { return nil }
func (u UnsupportedBackend) Stop()                              {}
func (u UnsupportedBackend) Pause(bool)                         {}
func (u UnsupportedBackend) Play(string) (chan struct{}, error) { return nil, ErrNotSupported }
func (u UnsupportedBackend) Paused() bool                       { return true }
func (u UnsupportedBackend) TogglePause()                       {}
func (u UnsupportedBackend) SetVolume(float64)                  {}
func (u UnsupportedBackend) IncreaseVolume(n float64)           {}
func (u UnsupportedBackend) Volume() float64                    { return 0 }
func (u UnsupportedBackend) Seek(time.Duration, int)            {}
func (u UnsupportedBackend) seek(int64)                         {}
func (u UnsupportedBackend) SeekTo(float64)                     {}
func (u UnsupportedBackend) Position() time.Duration            { return 0 }
func (u UnsupportedBackend) Duration() time.Duration            { return 0 }
