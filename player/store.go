package player

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"
)

func (p *Player) LoadPosition() error {
	f, err := os.Open(p.posFile)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()

	buf := make([]byte, 4)
	if _, err := io.ReadFull(f, buf); err != nil {
		return err
	}
	pos := binary.LittleEndian.Uint32(buf)
	dpos := time.Duration(pos) * time.Second

	if p.current == nil {
		p.Play()
		p.Seek(dpos, io.SeekStart)
		p.Pause()
	}
	return nil
}

func (p *Player) SavePosition() error {
	_pos := p.Position()
	if _pos < 0 {
		_pos = 0
	}
	pos := uint32(_pos / time.Second)

	stamp := strconv.FormatInt(time.Now().UnixNano(), 36)
	rnd := make([]byte, 32)
	_, err := io.ReadFull(rand.Reader, rnd)
	if err != nil {
		return err
	}

	tmp := fmt.Sprintf(
		"%s.%s-%s.tmp",
		p.posFile,
		stamp,
		base64.RawURLEncoding.EncodeToString(rnd),
	)

	f, err := os.Create(tmp)
	if err != nil {
		return err
	}

	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, pos)
	if _, err := f.Write(buf); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}

	f.Close()
	return os.Rename(tmp, p.posFile)
}
