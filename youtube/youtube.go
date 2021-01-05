package youtube

import (
	"net/http"
)

const ua = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/87.0.4280.88 Safari/537.36"

func safeReq(req *http.Request) *http.Request {
	req.Header.Set("User-Agent", ua)
	return req
}

func doReq(req *http.Request) (*http.Response, error) {
	return http.DefaultClient.Do(safeReq(req))
}
