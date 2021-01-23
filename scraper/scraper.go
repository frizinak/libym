package scraper

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"

	"github.com/PuerkitoBio/goquery"
)

const ua = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/87.0.4280.88 Safari/537.36"

type Callback func(uri *url.URL, doc *goquery.Document, depth, item, total int) error

type Errors []*Error

func (e Errors) Error() error {
	s := make([]string, len(e))
	for i := range e {
		if e[i] == nil || e[i].Err == nil {
			continue
		}
		s[i] = e[i].String()
	}
	if len(s) == 0 {
		return nil
	}
	return errors.New(strings.Join(s, "\n"))
}

type Error struct {
	URI string
	Err error
}

func (e *Error) String() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("[%s] %s", e.URI, e.Err)
}

type Config struct {
	Concurrency int
	MaxDepth    int
	Callback    Callback
	Client      *http.Client
}

type Scraper struct {
	c Config
}

func New(c Config) *Scraper {
	if c.Client == nil {
		c.Client = &http.Client{}
	}
	if c.Concurrency <= 0 {
		c.Concurrency = 1
	}
	if c.MaxDepth < 0 {
		c.MaxDepth = 0
	}
	return &Scraper{c: c}
}

type job struct {
	depth int
	uri   *url.URL
}

type result struct {
	depth int
	uri   *url.URL
	doc   *goquery.Document
	links []*url.URL
	err   error
}

func (r result) Error() *Error {
	if r.err == nil {
		return nil
	}
	return &Error{r.uri.String(), r.err}
}

func (s *Scraper) Scrape(uri string, cb Callback) Errors {
	return s.ScrapeContext(context.Background(), uri, cb)
}

func (s *Scraper) ScrapeContext(ctx context.Context, uri string, cb Callback) Errors {
	u, err := url.Parse(uri)
	if err != nil {
		return Errors{{uri, err}}
	}

	cbs := make([]Callback, 0, 2)
	if s.c.Callback != nil {
		cbs = append(cbs, s.c.Callback)
	}
	if cb != nil {
		cbs = append(cbs, cb)
	}

	jobs := make(chan job, s.c.Concurrency)
	results := make(chan result, s.c.Concurrency)

	var wg sync.WaitGroup
	for i := 0; i < s.c.Concurrency; i++ {
		wg.Add(1)
		go func() {
			for j := range jobs {
				doc, links, err := s.do(j.uri)
				results <- result{j.depth + 1, j.uri, doc, links, err}
			}
			wg.Done()
		}()
	}

	jobs <- job{0, u}

	errors := make(Errors, 0)
	queue := 1
	jobsQueue := make([]job, 0)
	done := make(map[string]struct{}, 100)

	var item, total = 0, 1
	handleResult := func(r result) bool {
		if err := r.Error(); err != nil {
			errors = append(errors, err)
			return false
		}
		for _, cb := range cbs {
			if err := cb(r.uri, r.doc, r.depth, item, total); err != nil {
				errors = append(errors, &Error{r.uri.String(), err})
			}
		}

		return true
	}

main:
	for {
	jobs:
		for len(jobsQueue) != 0 {
			select {
			case jobs <- jobsQueue[0]:
				jobsQueue = jobsQueue[1:]
			default:
				break jobs
			}
		}

		select {
		case <-ctx.Done():
			if err := ctx.Err(); err != nil {
				errors = append(errors, &Error{uri, err})
			}
			break main

		case result := <-results:
			queue--
			item++
			if !handleResult(result) {
				continue
			}
			if result.depth > s.c.MaxDepth {
				continue
			}

			for _, u := range result.links {
				uriStr := u.String()
				if _, ok := done[uriStr]; ok {
					continue
				}
				done[uriStr] = struct{}{}

				jobsQueue = append(jobsQueue, job{result.depth, u})
				queue++
				total++
			}
		default:
			if queue == 0 {
				break main
			}
		}
	}

	close(jobs)
	go func() {
		wg.Wait()
		close(results)
	}()
	for result := range results {
		handleResult(result)
	}

	return errors
}

func (s *Scraper) do(uri *url.URL) (*goquery.Document, []*url.URL, error) {
	src := *uri
	req, err := http.NewRequest("GET", uri.String(), nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", ua)
	s.c.Client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Host != uri.Host {
			return http.ErrUseLastResponse
		}
		return nil
	}

	res, err := s.c.Client.Do(req)
	if err != nil {
		return nil, nil, err
	}

	doc, err := goquery.NewDocumentFromReader(res.Body)
	res.Body.Close()
	if err != nil {
		return nil, nil, err
	}

	links := make([]*url.URL, 0)
	doc.Find("a[href]").Each(func(i int, s *goquery.Selection) {
		href, ok := s.Attr("href")
		if !ok || href == "" {
			return
		}
		href = strings.SplitN(href, "#", 2)[0]

		var next url.URL
		switch {
		case strings.HasPrefix(href, "//"):
			n, err := url.Parse(src.Scheme + href)
			if err == nil {
				next = *n
			}
		case strings.HasPrefix(href, "/"):
			next = src
			next.Path = href
		case strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://"):
			n, err := url.Parse(href)
			if err != nil {
				next = *n
			}
		default:
			next = src
			next.Path = path.Join(next.Path, href)
		}

		if next.Host != src.Host {
			return
		}

		links = append(links, &next)
	})

	return doc, links, err
}
