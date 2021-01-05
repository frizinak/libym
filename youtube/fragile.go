package youtube

import (
	"bufio"
	"bytes"
	"errors"
	"html"
	"io"
	"regexp"
	"strings"

	"github.com/antchfx/jsonquery"
)

type reRuneReader struct {
	maxlen int
	r      io.RuneReader
	re     *regexp.Regexp

	ix  int
	buf []rune
}

func (r *reRuneReader) String() ([]string, error) {
	res := r.re.FindReaderSubmatchIndex(r)
	if len(res) < 4 {
		return nil, nil
	}

	n := make([]string, 0, (len(res)-2)/2)
	buflen := len(r.buf)
	for i := 0; i < len(res); i += 2 {
		f := res[i+0] - r.ix + buflen
		e := res[i+1] - r.ix + buflen
		if f < 0 || e < 0 || f >= buflen || e >= buflen {
			return n, errors.New("out of range, buffer too small")
		}

		n = append(n, string(r.buf[f:e]))
	}

	return n, nil
}

func (r *reRuneReader) ReadRune() (rune, int, error) {
	rn, n, err := r.r.ReadRune()
	if err != nil {
		return rn, n, err
	}
	r.ix++
	r.buf = append(r.buf, rn)
	if len(r.buf) > r.maxlen {
		r.buf = r.buf[len(r.buf)-r.maxlen:]
	}
	return rn, n, err
}

func pageTitle(r io.Reader) (string, error) {
	// it's in fragile.go so this is allowed, trust me
	const buf = 1024
	re := regexp.MustCompile(`<title[^>]*>(.*?)</title`)

	// could be smaller, we only really need ReadRune, performance impact
	rr := bufio.NewReaderSize(r, buf)
	rrr := reRuneReader{maxlen: buf, r: rr, re: re}
	s, err := rrr.String()
	if err != nil || len(s) < 2 {
		return "", errors.New("no title found in page")
	}

	title := strings.TrimSpace(html.UnescapeString(s[1]))
	if len(title) < 10 && strings.Contains(s[1], "YouTube") {
		return "", errors.New("invalid page")
	}

	return title, nil
}

func parseSearch(r io.Reader) (Results, error) {
	const (
		pre  = 0
		post = 1
	)

	opener := ""
	closer := ""
	opened := 0
	onceOpened := false

	matching := map[string]string{
		"{": "}",
		"[": "]",
	}

	s := bufio.NewScanner(r)
	s.Split(bufio.ScanRunes)

	ytInitial := "ytInitialData ="
	ytInitialPos := 0

	buf := bytes.NewBuffer(nil)

	status := pre

	for s.Scan() {
		c := s.Text()

		switch status {
		case pre:
			if ytInitial[ytInitialPos] == c[0] {
				ytInitialPos++
				if ytInitialPos >= len(ytInitial) {
					status = post
				}
				continue
			}
			ytInitialPos = 0
			continue
		case post:
			if opener == "" {
				if n, ok := matching[c]; ok {
					opener = c
					closer = n
				}
			}

			if c == opener {
				onceOpened = true
				opened++
			} else if c == closer {
				opened--
			}
		}

		if onceOpened {
			buf.WriteString(c)
		}

		if onceOpened && opened == 0 {
			break
		}
	}

	if err := s.Err(); err != nil {
		return nil, err
	}

	return decodeSearch(buf)
}

func decodeSearch(r io.Reader) (Results, error) {
	rs := make(Results, 0)
	d, err := jsonquery.Parse(r)
	if err != nil {
		return nil, err
	}

	els := jsonquery.Find(d, "//videoId")
	for _, e := range els {
		if e.FirstChild == nil || e.FirstChild.Type != jsonquery.TextNode {
			continue
		}
		t := jsonquery.FindOne(e.Parent, "//title//text")
		if t == nil || t.FirstChild == nil || t.FirstChild.Type != jsonquery.TextNode {
			continue
		}

		rs = append(
			rs,
			NewResult(e.FirstChild.Data, t.FirstChild.Data),
		)
	}

	return rs, nil
}
