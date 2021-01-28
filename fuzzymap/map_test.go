package fuzzymap_test

import (
	"testing"

	"github.com/frizinak/libym/fuzzymap"
)

func TestIt(t *testing.T) {
	value := map[string]interface{}{
		"text": "some text",
		"items": []interface{}{
			"item one",
			map[string]interface{}{
				"title": "extended item two",
				"body":  struct{ Contents string }{"body contents"},
			},
		},
		"links": map[string]interface{}{
			"home":  "https://www.homepage.org",
			"about": []interface{}{"one", "two"},
			"body":  "link body?",
		},
		"slices": []interface{}{
			[]interface{}{
				"1.1",
				"1.2",
				[]interface{}{
					"1.3.1",
					"1.3.2",
					[]interface{}{
						"1.3.3.1",
						"1.3.3.2",
					},
					"1.3.4",
				},
			},
			[]interface{}{
				"2.1",
				"2.2",
				[]interface{}{
					"2.3.1",
					"2.3.2",
					[]interface{}{
						"2.3.3.1",
						"2.3.3.2",
					},
					"2.3.4",
				},
			},
		},
	}

	m := fuzzymap.New(value)
	m.SortRecursive()

	results := m.Filter("body")
	if len(results) != 2 {
		t.Fatal("expected 2 results", results)
	}

	results = m.Filter("items", "body")
	if len(results) != 1 {
		t.Fatal("expected 1 result", results)
	}

	results = m.Filter("0")
	if len(results) != 0 {
		t.Fatal("expected 0 results", results)
	}

	if m.Filter("links", "home")[0].Parent.Key != "links" {
		t.Fatal("expected a different parent for links.home")
	}

	t.Log(m.Filter("items", "body")[0].Value.(struct{ Contents string }).Contents)

	if m.Filter("items", "body")[0].Parent.Parent.Key != "items" {
		t.Fatal("expected a different parent", m.Filter("items", "body")[0].Parent)
	}
	results = m.Filter("slices")
	if len(results) != 1 {
		t.Fatal("expected one slice result")
	}

	var rec func(depth int, m fuzzymap.M)
	rec = func(depth int, m fuzzymap.M) {
		for _, v := range m {
			rec(depth+1, v.Children)
			p := v
			for i := 0; i < depth; i++ {
				p = p.Parent
				if p == nil {
					t.Fatal("nil parent at:", depth)
				}
			}
			if p != results[0] {
				t.Fatal("parent depth not correct at:", depth)
			}
		}
	}

	rec(0, results)
}
