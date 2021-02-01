package collection

import (
	"net/url"
	"os"

	"github.com/frizinak/binary"
	"github.com/frizinak/libym/youtube"
)

type YoutubeSong struct {
	*youtube.Result
	file string
}

const NSYoutube = "yt"

func (s *YoutubeSong) NS() string { return NSYoutube }

func (s *YoutubeSong) Marshal(w *binary.Writer) error {
	w.WriteString(s.ID(), 8)
	w.WriteString(s.Title(), 16)
	return w.Err()
}

func YoutubeSongUnmarshal(c *Collection, dec *binary.Reader) (*YoutubeSong, error) {
	id := dec.ReadString(8)
	title := dec.ReadString(16)
	if err := dec.Err(); err != nil {
		return nil, err
	}
	s := c.FromYoutube(youtube.NewResult(id, title))
	return s, nil
}

func (s *YoutubeSong) Local() bool {
	_, err := os.Stat(s.file)
	return err == nil
}

func (s *YoutubeSong) File() (string, error)      { return s.file, nil }
func (s *YoutubeSong) URL() (*url.URL, error)     { return s.DownloadURL() }
func (s *YoutubeSong) PageURL() (*url.URL, error) { return s.Result.URL(), nil }
