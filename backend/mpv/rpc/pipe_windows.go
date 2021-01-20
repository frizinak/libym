// +build windows

package rpc

import (
	"strings"

	"gopkg.in/natefinch/npipe.v2"
)

func Dial(path string) (Conn, error) {
	return npipe.Dial(path)
}

func Pipe(path string) string {
	switch {
	case strings.HasPrefix(path, "\\\\.\\pipe\\"):
	default:
		path = "\\\\.\\pipe\\" + strings.TrimLeft(strings.ReplaceAll(path, "/", "\\"), "\\")
	}

	return path
}
