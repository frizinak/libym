package collection

import (
	"net/url"
	"os"

	"github.com/frizinak/binary"
	"github.com/frizinak/libym/youtube"
)

type YoutubeSong struct {
	r    *youtube.Result
	file string
	g    string
}

const NSYoutube = "yt"

func (s *YoutubeSong) NS() string { return NSYoutube }
func (s *YoutubeSong) ID() string { return s.r.ID() }
func (s *YoutubeSong) GlobalID() string {
	if s.g == "" {
		s.g = GlobalID(s)
	}
	return s.g
}

func (s *YoutubeSong) Title() string { return s.r.Title() }

func (s *YoutubeSong) Marshal(w *binary.Writer) error {
	w.WriteString(s.r.ID(), 8)
	w.WriteString(s.r.Title(), 16)
	return w.Err()
}

func (s *YoutubeSong) Local() bool {
	_, err := os.Stat(s.file)
	return err == nil
}

func (s *YoutubeSong) File() (string, error) {
	return s.file, nil
}

func (s *YoutubeSong) URL() (*url.URL, error) {
	return s.r.DownloadURL()
}
