package youtube

import (
	"bufio"
	"encoding/json"
	"errors"
	"html"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/frizinak/libym/fuzzymap"
)

type reRuneReader struct {
	maxlen int
	r      io.RuneReader
	re     *regexp.Regexp

	ix      int
	buf     []byte
	runeBuf []byte
}

func (r *reRuneReader) String() ([]string, error) {
	r.runeBuf = make([]byte, 4)
	r.buf = make([]byte, 0, r.maxlen)

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
	utf8.EncodeRune(r.runeBuf, rn)
	r.ix += n
	r.buf = append(r.buf, r.runeBuf[:n]...)
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
	if len(title) < 10 && strings.Contains(title, "YouTube") {
		return "", errors.New("invalid page")
	}

	const suff = " - YouTube"
	if strings.HasSuffix(title, suff) {
		title = title[:len(title)-len(suff)]
	}

	return title, nil
}

func parseYTInitialData(r io.Reader) (fuzzymap.M, io.Reader, error) {
	ytInitial := []rune("ytInitialData =")
	ytInitialPos := 0

	rr := bufio.NewReader(r)
	for {
		rn, _, err := rr.ReadRune()
		if err != nil {
			return nil, r, err
		}

		if ytInitial[ytInitialPos] == rn {
			ytInitialPos++
			if ytInitialPos >= len(ytInitial) {
				break
			}
			continue
		}
		ytInitialPos = 0
	}

	m := make(map[string]interface{})
	dec := json.NewDecoder(rr)
	err := dec.Decode(&m)
	nr := io.MultiReader(dec.Buffered(), r)
	if err != nil {
		return nil, nr, err
	}

	return fuzzymap.New(m), nr, err
}

func parseSearch(r io.Reader) ([]*Result, io.Reader, error) {
	m, nr, err := parseYTInitialData(r)
	if err != nil {
		return nil, nr, err
	}

	res, err := decodeSearch(m)
	return res, nr, err
}

func decodeSearch(m fuzzymap.M) ([]*Result, error) {
	rs := make([]*Result, 0)
	els := m.Filter("videoId")
	for _, e := range els {
		if e.Parent == nil {
			continue
		}

		vid, ok := e.Value.(string)
		if !ok || len(e.Children) != 0 {
			continue
		}

		titles := e.Parent.Children.Filter("title", "text")
		if len(titles) != 1 {
			continue
		}
		title, ok := titles[0].Value.(string)
		if !ok || len(titles[0].Children) != 0 {
			continue
		}

		hasLiveBadge := false
		badgeStyles := e.Parent.Children.Filter("badges", "style")
		for _, bs := range badgeStyles {
			if style, ok := bs.Value.(string); ok && strings.Contains(style, "_LIVE") {
				hasLiveBadge = true
				break
			}
		}

		if hasLiveBadge {
			continue
		}

		rs = append(
			rs,
			NewResult(vid, title),
		)
	}

	return rs, nil
}
