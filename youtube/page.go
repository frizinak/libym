package youtube

import (
	"net/http"
	"net/url"
)

func Page(id string) (*url.URL, error) {
	u, err := url.Parse("https://www.youtube.com/watch")
	if err != nil {
		return u, err
	}

	qry := u.Query()
	qry.Set("v", id)
	u.RawQuery = qry.Encode()
	return u, err
}

// Title extracts the page title of the given youtube clip id.
func Title(id string) (string, error) {
	u, err := Page(id)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return "", err
	}

	res, err := doReq(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	return pageTitle(res.Body)
}
