package collection

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/frizinak/binary"
)

const (
	storeQueue = "__QUEUE\x00\x01\x08"
	eos        = "__eos\x00\x01\x08"
)

type Unmarshaler func(dec *binary.Reader) (Song, error)

func (c *Collection) RegisterUnmarshaler(ns string, unmarshaler Unmarshaler) {
	c.unmarshalers[ns] = unmarshaler
}

func (c *Collection) Load() error {
	db, err := os.Open(c.pathDB())
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer db.Close()
	reader, err := gzip.NewReader(db)
	if err != nil {
		return err
	}
	defer reader.Close()

	dec := binary.NewReader(reader)
	songs := make(map[string]Song)

	nsongs := dec.ReadUint64()

	var i uint64
	for ; i < nsongs; i++ {
		ns := dec.ReadString(8)
		if _, ok := c.unmarshalers[ns]; !ok {
			return fmt.Errorf("no unmarshaler for namespace '%s'", ns)
		}

		song, err := c.unmarshalers[ns](dec)
		if err != nil {
			return err
		}
		songs[GlobalID(song)] = song
	}

	getSongs := func() ([]Song, error) {
		errs := make([]string, 0)
		nq := dec.ReadUint32()
		l := make([]Song, 0, nq)
		var i uint32
		for ; i < nq; i++ {
			g := dec.ReadString(8)
			song, ok := songs[g]
			if !ok {
				errs = append(errs, fmt.Sprintf("could not find song with id %s", g))
				continue
			}
			l = append(l, song)
		}

		if len(errs) != 0 {
			return l, errors.New(strings.Join(errs, "\n"))
		}
		return l, nil
	}

	for {
		playlist := dec.ReadString(16)
		if playlist == "" {
			if dec.Err() == io.EOF {
				return nil
			}
			return errors.New("empty playlistname?")
		}

		if playlist == eos {
			break
		}

		if playlist == storeQueue {
			l, err := getSongs()
			if err != nil {
				return err
			}
			for _, s := range l {
				c.QueueSong(s)
			}
			continue
		}

		if err := c.Create(playlist); err != nil {
			return err
		}

		l, err := getSongs()
		if err != nil {
			return err
		}

		for _, s := range l {
			if err := c.AddSong(playlist, s); err != nil {
				return err
			}
		}
	}

	if err := dec.Err(); err != nil {
		return err
	}
	ix := dec.ReadUint32()
	c.q.SetCurrentIndex(int(ix))
	if err := dec.Err(); err != io.EOF {
		return err
	}

	return nil
}

func (c *Collection) Save() error {
	c.sem.Lock()
	defer c.sem.Unlock()
	path := c.pathDB()
	tmp := TempFile(path)
	db, err := os.Create(tmp)
	if err != nil {
		return err
	}

	ix := uint32(c.q.CurrentIndex())
	c.q.sem.Lock()
	defer c.q.sem.Unlock()

	index := make(map[string]Song)
	for _, p := range c.playlists {
		p.sem.Lock()
		defer p.sem.Unlock()

		songs := p.songs
		for _, s := range songs {
			index[GlobalID(s)] = s
		}
	}

	q := c.q.slice()
	for _, s := range q {
		index[GlobalID(s)] = s
	}

	do := func() error {
		writer, err := gzip.NewWriterLevel(db, gzip.BestSpeed)
		if err != nil {
			return err
		}
		defer writer.Close()
		enc := binary.NewWriter(writer)
		enc.WriteUint64(uint64(len(index)))
		for _, s := range index {
			enc.WriteString(s.NS(), 8)
			if err := s.Marshal(enc); err != nil {
				return err
			}
		}

		for i, p := range c.playlists {
			enc.WriteString(i, 16)
			songs := p.songs
			enc.WriteUint32(uint32(len(songs)))
			for _, s := range songs {
				enc.WriteString(GlobalID(s), 8)
			}
		}

		enc.WriteString(storeQueue, 16)
		enc.WriteUint32(uint32(len(q)))
		for _, s := range q {
			enc.WriteString(GlobalID(s), 8)
		}

		enc.WriteString(eos, 16)
		enc.WriteUint32(ix)

		return enc.Err()
	}

	if err := do(); err != nil {
		db.Close()
		os.Remove(tmp)
		return err
	}

	db.Close()
	return os.Rename(tmp, path)
}
