package acoustid

import (
	"errors"
	"fmt"
	"strings"
)

type Response struct {
	Status  string    `json:"status"`
	Results []*Result `json:"results"`
	Error   struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (r *Response) OK() bool { return r.Status == "ok" }
func (r *Response) Err() error {
	if r.OK() {
		return nil
	}

	if r.Error.Message == "" {
		return errors.New("invalid response, no error given")
	}

	return fmt.Errorf("%d: %s", r.Error.Code, r.Error.Message)
}

func (r *Response) Best(minScore float32) (result *Result, ok bool) {
	var max float32
	for _, r := range r.Results {
		if len(r.Recordings) != 0 &&
			r.Recordings[0].Title != "" &&
			r.Score > max &&
			r.Score > minScore {

			ok = true
			result = r
		}
	}

	return
}

func (r *Response) BestString(minScore float32) string {
	b, ok := r.Best(minScore)
	if !ok {
		return ""
	}

	return b.Recordings[0].String()
}

type Result struct {
	ID         string       `json:"id"`
	Score      float32      `json:"score"`
	Recordings []*Recording `json:"recordings"`
}

type Recording struct {
	ID            string        `json:"id"`
	Duration      int           `json:"duration"`
	Title         string        `json:"title"`
	ReleaseGroups ReleaseGroups `json:"releasegroups"`
	Artists       Artists       `json:"artists"`
}

func (r *Recording) String() string {
	return r.ToString("%{artists} - %{title}%{album? [%]:}")
}

func (r *Recording) ToString(format string) string {
	n := format
	repls := map[string]func() string{
		"artists": func() string { return r.Artists.String() },
		"artist":  func() string { return r.Artists.String() },
		"title":   func() string { return r.Title },
		"album": func() string {
			if len(r.ReleaseGroups) == 0 {
				return ""
			}

			albums := r.ReleaseGroups.Filter(Album)
			l := []ReleaseGroups{
				albums.FilterNot(Compilation),
				albums,
			}
			for _, rs := range l {
				if len(rs) != 0 {
					return rs[0].Title
				}
			}

			return r.ReleaseGroups[0].Title
		},
	}

	for r, cb := range repls {
		start := "%{" + r
		if !strings.Contains(format, start) {
			continue
		}

		ix := strings.Index(n, start)
		rest := n[ix+len(start):]
		if len(rest) == 0 {
			continue
		}

		if rest[0] == '}' {
			n = strings.Replace(n, start+"}", cb(), 1)
		} else if rest[0] == '?' {
			match := []rune(start)
			condTrue := make([]rune, 0, 1)
			condFalse := make([]rune, 0, 1)
			phase := 0
			escaped := false
			for i, n := range rest {
				match = append(match, n)
				if !escaped && n == '}' {
					break
				}

				if !escaped && n == ':' {
					phase = 1
					n = 0
				}

				if i != 0 && n > 0 && (escaped || n != '\\') {
					if phase == 0 {
						condTrue = append(condTrue, n)
					} else if phase == 1 {
						condFalse = append(condFalse, n)
					}
				}

				escaped = !escaped && n == '\\'
			}

			value := cb()
			cond := condTrue
			if value == "" {
				cond = condFalse
			}

			value = strings.Replace(string(cond), "%", value, 1)
			n = strings.Replace(n, string(match), value, 1)
		}
	}

	return n
}

func (r *Recording) Album() string {
	if len(r.ReleaseGroups) == 0 {
		return ""
	}

	return r.ReleaseGroups[0].Title
}

type ReleaseGroups []*ReleaseGroup

type ReleaseType string

const Album ReleaseType = "Album"
const Single ReleaseType = "Single"
const Compilation ReleaseType = "Compilation"

func (r ReleaseGroups) FilterNot(t ...ReleaseType) ReleaseGroups {
	m := make(map[ReleaseType]struct{}, len(t))
	for i := range t {
		m[t[i]] = struct{}{}
	}

	n := make(ReleaseGroups, 0, len(r))
	for _, rg := range r {
		match := true
		if _, ok := m[rg.Type]; ok {
			match = false
		}
		for _, sec := range rg.SecondaryTypes {
			if _, ok := m[sec]; ok {
				match = false
				break
			}
		}
		if match {
			n = append(n, rg)
		}
	}

	return n
}

func (r ReleaseGroups) Filter(t ...ReleaseType) ReleaseGroups {
	m := make(map[ReleaseType]struct{}, len(t))
	for i := range t {
		m[t[i]] = struct{}{}
	}

	n := make(ReleaseGroups, 0, len(r))
outer:
	for _, rg := range r {
		if _, ok := m[rg.Type]; ok {
			n = append(n, rg)
			continue
		}

		for _, sec := range rg.SecondaryTypes {
			if _, ok := m[sec]; ok {
				n = append(n, rg)
				continue outer
			}
		}
	}

	return n
}

type ReleaseGroup struct {
	ID             string        `json:"id"`
	Type           ReleaseType   `json:"type"`
	SecondaryTypes []ReleaseType `json:"secondarytypes"`
	Title          string        `json:"title"`
}

type Artists []*Artist

func (a Artists) String() string {
	s := make([]string, 0, len(a))
	for _, artist := range a {
		s = append(s, artist.Name)
		if artist.Joinphrase != "" {
			s = append(s, artist.Joinphrase)
		}
	}

	return strings.Join(s, "")
}

// func (a Artists) Sort()              { sort.Stable(a) }
// func (a Artists) Len() int           { return len(a) }
// func (a Artists) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
// func (a Artists) Less(i, j int) bool { return a[i].Joinphrase < a[j].Joinphrase }

// func (a Artists) String() string { return a.ToString(", ") }

// func (a Artists) ToString(glue string) string {
// 	joins := make(map[string][]string)
// 	keys := make([]string, 0, 1)
//
// 	for _, art := range a {
// 		n := strings.TrimSpace(art.Name)
// 		if n == "" {
// 			continue
// 		}
//
// 		k := strings.TrimSpace(art.Joinphrase)
// 		keys = append(keys, k)
// 		if _, ok := joins[k]; !ok {
// 			joins[k] = make([]string, 0, 1)
// 		}
// 		joins[k] = append(joins[k], n)
// 	}
//
// 	sort.Strings(keys)
// 	strs := make([]string, 0, 1)
// 	for _, k := range keys {
// 		if k != "" {
// 			strs = append(strs, k)
// 		}
// 		strs = append(strs, strings.Join(joins[k], glue))
// 	}
//
// 	return strings.Join(strs, " ")
// }

type Artist struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Joinphrase string `json:"joinphrase"`
}
