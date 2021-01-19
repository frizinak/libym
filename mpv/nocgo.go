// +build !cgo

package mpv

import (
	"log"

	"github.com/frizinak/libym/player"
)

func New(log *log.Logger) *LibMPV {
	return &LibMPV{}
}

type LibMPV struct {
	player.UnsupportedBackend
}

func (m *LibMPV) Init() error { return player.ErrNotSupported }
