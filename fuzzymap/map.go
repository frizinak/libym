package fuzzymap

import (
	"fmt"
	"sort"
	"strings"
)

type M []*Value

type Value struct {
	Key      string
	Parent   *Value
	Children M
	Value    interface{}
}

func (v *Value) String() string {
	return fmt.Sprint(v.Value)
}

func createPlain(parent *Value, input interface{}) *Value {
	cur := &Value{Parent: parent}
	switch rv := input.(type) {
	case map[string]interface{}:
		cur.Children = create(cur, rv)
	case []interface{}:
		cur.Children = make(M, len(rv))
		for i := range rv {
			cur.Children[i] = createPlain(cur, rv[i])
		}
	default:
		cur.Value = rv
	}

	return cur
}

func create(parent *Value, input map[string]interface{}) M {
	m := make(M, 0, len(input))
	for k, v := range input {
		cur := createPlain(parent, v)
		cur.Key = k
		m = append(m, cur)
	}

	return m
}

func New(input map[string]interface{}) M {
	return create(nil, input)
}

func (m M) Filter(keys ...string) M {
	results := make(M, 0)
	if len(keys) == 0 {
		return results
	}

	for _, v := range m {
		if v.Key == keys[0] {
			if len(keys) == 1 {
				results = append(results, v)
				continue
			}
			results = append(results, v.Children.Filter(keys[1:]...)...)
		}
		results = append(results, v.Children.Filter(keys...)...)
	}

	return results
}

func (m M) Len() int           { return len(m) }
func (m M) Swap(i, j int)      { m[i], m[j] = m[j], m[i] }
func (m M) Less(i, j int) bool { return m[i].Key < m[j].Key }

func (m M) SortRecursive() {
	sort.Sort(m)
	for _, v := range m {
		v.Children.SortRecursive()
	}
}

func (m M) String() string {
	l := make([]string, 0)
	var print func(string, M)
	print = func(prefix string, m M) {
		for i, v := range m {
			l = append(l, fmt.Sprintf("%s%-3d%s (%T)", prefix, i, v.Key, v.Value))
			if len(v.Children) == 0 {
				l = append(l, fmt.Sprintf("%s%-6s%+v", prefix, "", v.Value))
				continue
			}
			print(prefix+"  ", v.Children)
		}
	}

	print("", m)
	return strings.Join(l, "\n")
}
