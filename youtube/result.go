package youtube

import (
	"bytes"
	"fmt"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
)

type Results []*Result
type Result struct {
	videoID string
	title   string
	u       *url.URL
}

func NewResult(id, title string) *Result {
	return &Result{videoID: id, title: title}
}

func (r *Result) ID() string    { return r.videoID }
func (r *Result) Title() string { return r.title }

func (r *Result) URL() *url.URL {
	if r.u != nil {
		return r.u
	}

	u, err := url.Parse("https://www.youtube.com/watch")
	if err != nil {
		panic(err)
	}
	qry := u.Query()
	qry.Set("v", r.videoID)
	u.RawQuery = qry.Encode()
	r.u = u
	return u
}

func (r *Result) DownloadURL() (*url.URL, error) {
	cmd := exec.Command("youtube-dl", "-g", "-f", "bestaudio", r.URL().String())
	buf := bytes.NewBuffer(nil)
	bufe := bytes.NewBuffer(nil)
	cmd.Stdout = buf
	cmd.Stderr = bufe
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%w: %s", err, bufe.String())
	}

	return url.Parse(strings.TrimSpace(buf.String()))
}

func (r *Result) UpdateTitle() error {
	n, err := Title(r.ID())
	if err != nil {
		return fmt.Errorf("%s: %w", r.ID(), err)
	}
	if n == "" {
		return fmt.Errorf("%s: received empty title", r.ID())
	}
	r.title = n
	return nil
}

var schemeRE = regexp.MustCompile(`^(https?://)|^(//)?`)

func FromURL(u, title string) (*Result, error) {
	r := &Result{title: title}

	u = schemeRE.ReplaceAllString(u, "https://")
	n, err := url.Parse(u)
	if err != nil {
		return nil, err
	}

	direct := false
	switch n.Hostname() {
	case "youtu.be":
		direct = true
	case "www.youtube.com", "m.youtube.com", "youtube.com":
	default:
		return nil, fmt.Errorf("'%s' seems to not be a youtube url", u)
	}

	p := strings.Split(n.Path, "/")
	q := n.Query()

	if len(p) > 1 && (p[1] == "embed" || p[1] == "v") {
		if len(p) > 2 {
			r.videoID = p[2]
			return r, nil
		}
	}

	if v := q.Get("v"); v != "" {
		r.videoID = v
		return r, nil
	}

	if direct {
		if len(p) > 1 {
			r.videoID = p[1]
			return r, nil
		}
	}

	return nil, fmt.Errorf("'%s' does not seem to be a youtube video url", u)
}
