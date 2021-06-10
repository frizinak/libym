package rpc

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/frizinak/libym/backend/mpv"
)

func New(log *log.Logger, ipcPath string, flags []string) *mpv.MPV {
	return mpv.New(
		log,
		&RPC{
			cmd:       "mpv",
			flags:     flags,
			ipc:       Pipe(ipcPath),
			responses: make(chan response, 1024),
		},
	)
}

type command struct {
	Command   []interface{} `json:"command"`
	RequestID uint16        `json:"request_id,omitempty"`
}

func newSimpleCommand(cmd string, args ...interface{}) command {
	cmds := make([]interface{}, len(args)+1)
	cmds[0] = cmd
	copy(cmds[1:], args)
	return command{Command: cmds}
}

func newCommand(id uint16, cmd string, args ...interface{}) command {
	c := newSimpleCommand(cmd, args...)
	c.RequestID = id
	return c
}

type response struct {
	Event     string      `json:"event"`
	Error     string      `json:"error"`
	Data      interface{} `json:"data"`
	RequestID uint16      `json:"request_id,omitempty"`
}

type RPC struct {
	sem sync.Mutex

	cmd   string
	flags []string
	ipc   string

	command *exec.Cmd
	conn    Conn
	w       *json.Encoder

	n uint16

	responses chan response
}

func (m *RPC) Init(events chan<- mpv.Event) error {
	os.MkdirAll(filepath.Dir(m.ipc), 0755)
	f := []string{
		"--no-video",
		"--idle",
		"--no-config",
		"--input-ipc-server=" + m.ipc,
	}
	f = append(f, m.flags...)

	m.command = exec.Command(m.cmd, f...)

	if err := m.command.Start(); err != nil {
		return err
	}

	n := 0
	for {
		time.Sleep(time.Millisecond * 25)

		var err error
		m.conn, err = Dial(m.ipc)
		if err == nil {
			break
		}
		n++
		if n > 100 {
			m.command.Process.Kill()
			return err
		}
	}

	m.w = json.NewEncoder(m.conn)
	d := json.NewDecoder(m.conn)

	ev := map[string]mpv.EventID{
		"end-file":   mpv.EventEndFile,
		"start-file": mpv.EventStartFile,
		"pause":      mpv.EventPause,
		"unpause":    mpv.EventUnpause,
	}

	go func() {
		for {
			r := response{}
			if err := d.Decode(&r); err != nil {
				continue
			}

			if r.Event == "" {
				m.responses <- r
				continue
			}

			if id, ok := ev[r.Event]; ok {
				events <- mpv.Event{id}
			}

		}
	}()

	return nil
}

func (m *RPC) Close() error {
	if m.conn != nil {
		m.conn.Close()
	}

	return m.command.Process.Kill()
}

func (m *RPC) req() uint16 {
	m.sem.Lock()
	m.n++
	if m.n == 0 {
		m.n = 1
	}
	r := m.n
	m.sem.Unlock()

	return r
}

func (m *RPC) waitReq(n uint16) (response, error) {
	for r := range m.responses {
		if r.RequestID == n {
			var err error
			if r.Error != "" && r.Error != "success" {
				err = errors.New(r.Error)
			}
			return r, err
		}
		m.responses <- r
	}

	panic("closed")
}

func (m *RPC) GetProperty(n string) (interface{}, error) {
	req := m.req()
	if err := m.send(newCommand(req, "get_property", n)); err != nil {
		return nil, err
	}
	response, err := m.waitReq(req)
	return response.Data, err
}

func (m *RPC) GetPropertyDouble(n string) (float64, error) {
	_v, err := m.GetProperty(n)
	if err != nil || _v == nil {
		return 0, err
	}

	switch v := _v.(type) {
	case float64:
		return v, nil
	case int64:
		return float64(v), nil
	case int:
		return float64(v), nil
	default:
		return 0, fmt.Errorf("%s is not a float", n)
	}
}

func (m *RPC) SetPropertyDouble(n string, v float64) error {
	return m.send(newSimpleCommand("set_property", n, v))
}

func (m *RPC) SetPropertyString(n, v string) error {
	return m.send(newSimpleCommand("set_property", n, v))
}

func (m *RPC) SetPropertyBool(n string, v bool) error {
	return m.send(newSimpleCommand("set_property", n, v))
}

func (m *RPC) Command(v ...string) error {
	n := make([]interface{}, len(v))
	for i, val := range v {
		n[i] = val
	}

	return m.send(command{Command: n})
}

func (m *RPC) send(cmd command) error { return m.w.Encode(cmd) }
