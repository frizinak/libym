// +build !windows

package rpc

import "net"

func Dial(path string) (Conn, error) {
	return net.Dial("unix", path)
}

func Pipe(path string) string {
	return path
}
