// +build !cgo

package lib

import (
	"log"

	"github.com/frizinak/libym/player"
)

func New(log *log.Logger) (p player.UnsupportedBackend) { return }
