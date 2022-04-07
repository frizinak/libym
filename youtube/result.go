// Package youtube provides a few utilities around youtube clips.

package youtube

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os/exec"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/frizinak/libym/scraper"
)

// Result represents a youtube.com search result, i.e.: a youtube clip.
type Result struct {
	videoID string
	title   string
	u       *url.URL
}

// NewResult creates a new youtube result.
// ID is required and should not be empty for it to be a valid youtube clip.
// Title is an arbitrary string that will be used as the title,
// this can be fetched using Title(id string) or Result.UpdateTitle().
func NewResult(id, title string) *Result {
	return &Result{videoID: id, title: title}
}

// ID returns a the clip id.
func (r *Result) ID() string { return r.videoID }

// Title returns the title associated with this Result.
func (r *Result) Title() string { return r.title }

// URL constructs the youtube url for this clip.
func (r *Result) URL() *url.URL {
	if r.u != nil {
		return r.u
	}

	u, err := Page(r.videoID)
	if err != nil {
		panic(err)
	}
	r.u = u
	return u
}

// SetTitle updates the title.
func (r *Result) SetTitle(title string) { r.title = title }

// DownloadURL asks youtube-dl to create a (temporary) download / stream url
// of the clip's contents.
func (r *Result) DownloadURL() (*url.URL, error) {
	cmd := exec.Command("youtube-dl", "-g", "-f", "bestaudio", "--no-playlist", r.URL().String())
	buf := bytes.NewBuffer(nil)
	bufe := bytes.NewBuffer(nil)
	cmd.Stdout = buf
	cmd.Stderr = bufe
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(bufe.String()))
	}

	return url.Parse(strings.TrimSpace(buf.String()))
}

// UpdateTitle uses Title to update the clips title using its id.
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

// FromURL parses the given url to extract the id and create a youtube
// result. see NewResult.
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

// Scraper is a wrapper around github.com/frizinak/libym/scraper to extract
// youtube Results.
type Scraper struct {
	s  *scraper.Scraper
	cb *ScraperCallback
}

// NewScraper creates a new youtube url scraper with the given scraper.
// cb will be called with each match after a call to Scrape or
// ScrapeWithContext.
func NewScraper(s *scraper.Scraper, cb func(*Result)) *Scraper {
	return &Scraper{s: s, cb: NewScraperCallback(cb)}
}

// Scrape calls ScrapeWithContext without context.
func (s *Scraper) Scrape(uri string) error {
	return s.ScrapeWithContext(context.Background(), uri)
}

// Scrape start the scrape of the given url and can be canceled using ctx.
func (s *Scraper) ScrapeWithContext(ctx context.Context, uri string) error {
	return s.s.ScrapeWithContext(ctx, uri, s.cb.Callback).Error()
}

// ScraperCallback is the actual url matcher for Scraper which you probably
// want to use.
type ScraperCallback struct {
	re   *regexp.Regexp
	uniq map[string]struct{}
	cb   func(*Result)
}

// NewScraperCallback creates a new ScraperCallback.
func NewScraperCallback(cb func(*Result)) *ScraperCallback {
	return &ScraperCallback{
		cb:   cb,
		re:   regexp.MustCompile(`(?i)https?://(?:m\.|www\.)?youtu[a-z0-9\-_\./]+`),
		uniq: make(map[string]struct{}),
	}
}

// Callback is the actual function that can be passed to a
// github.com/frizinak/libym/scraper.Scraper.
func (s *ScraperCallback) Callback(uri *url.URL, doc *goquery.Document, depth, item, total int) error {
	html, err := doc.Html()
	if err != nil {
		return err
	}
	for _, u := range s.re.FindAllString(html, -1) {
		result, err := FromURL(u, "")
		if err != nil {
			continue
		}
		id := result.ID()
		if _, ok := s.uniq[id]; ok {
			continue
		}
		s.uniq[id] = struct{}{}
		s.cb(result)
	}

	return nil
}
