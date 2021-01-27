package youtube

import (
	"net/http"
	"net/url"
)

// Search queries youtube.com for search results matching the given query.
func Search(q string) ([]*Result, error) {
	u, err := url.Parse("https://www.youtube.com/results")
	if err != nil {
		return nil, err
	}

	qry := u.Query()

	qry.Set("search_query", q)
	u.RawQuery = qry.Encode()

	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return nil, err
	}

	res, err := doReq(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	return parseSearch(res.Body)
}
