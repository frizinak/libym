package acoustid

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const rate = 16000
const pref = "FINGERPRINT="
const ua = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/87.0.4280.88 Safari/537.36"

type Client struct {
	base      *url.URL
	http      *http.Client
	ffmpeg    string
	fpcalc    string
	ratelimit <-chan struct{}
	debugErr  bool
	debugAll  bool
}

type Config struct {
	Key        string
	FFMPEGBin  string
	FPCALCBin  string
	HTTPClient *http.Client
	DebugErr   bool
	DebugAll   bool
}

func New(c Config) (*Client, error) {
	if c.Key == "" {
		return nil, errors.New("no client key provided")
	}

	base, err := url.Parse(
		fmt.Sprintf(
			"https://api.acoustid.org/v2/lookup?format=json&client=%s&meta=recordings+releasegroups",
			c.Key,
		),
	)

	if err != nil {
		return nil, err
	}

	if c.FFMPEGBin == "" {
		c.FFMPEGBin = "ffmpeg"
	}

	if c.FPCALCBin == "" {
		c.FPCALCBin = "fpcalc"
	}

	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{
			Transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment,
				DialContext: (&net.Dialer{
					Timeout:   30 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				ForceAttemptHTTP2:     false,
				MaxIdleConns:          10,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
			},
		}
	}

	bin, err := exec.LookPath(c.FFMPEGBin)
	if err != nil {
		return nil, errors.New("no valid ffmpeg binary in $PATH")
	}
	c.FFMPEGBin = bin

	bin, err = exec.LookPath(c.FPCALCBin)
	if err != nil {
		return nil, errors.New("no valid fpcalc (chromaprint) binary in $PATH")
	}
	c.FPCALCBin = bin

	rl := make(chan struct{})
	go func() {
		// Rate limiting — Do not make more than 3 requests per second.
		//                            - https://acoustid.org/webservice
		for {
			rl <- struct{}{}
			time.Sleep(time.Second / 3)
		}
	}()

	return &Client{
		base,
		c.HTTPClient,
		c.FFMPEGBin,
		c.FPCALCBin,
		rl,
		c.DebugErr || c.DebugAll,
		c.DebugAll,
	}, nil
}

func (c *Client) Lookup(ctx context.Context, fingerprint string, duration int) (*Response, error) {
	u := *c.base

	q := u.Query()
	q.Set("fingerprint", fingerprint)
	q.Set("duration", strconv.Itoa(duration))
	u.RawQuery = q.Encode()

	<-c.ratelimit

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", ua)

	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	var rr io.Reader = res.Body
	var buf *bytes.Buffer
	if c.debugErr {
		buf = bytes.NewBuffer(nil)
		rr = io.TeeReader(res.Body, buf)
	}

	j := json.NewDecoder(rr)
	r := &Response{}
	err = j.Decode(r)
	if err == nil && c.debugAll {
		err = errors.New("debug")
	}
	if err != nil {
		if c.debugErr {
			return r, fmt.Errorf("%w: %s", err, buf.String())
		}

		return r, err
	}

	return r, r.Err()
}

func (c *Client) Fingerprint(ctx context.Context, r io.Reader) (string, int, error) {
	convert := exec.Command(
		c.ffmpeg,
		"-i", "-",
		"-f", "wav",
		"-map", "0:a:0",
		"-codec:a", "pcm_s16le",
		"-ar", strconv.Itoa(rate),
		"-",
	)

	fpcalc := exec.Command(
		c.fpcalc,
		"-rate", strconv.Itoa(rate),
		"-length", "240",
		"-",
	)

	convert.Stdin = r
	convertOut, _ := convert.StdoutPipe()
	fpcalcIn, _ := fpcalc.StdinPipe()
	tee := &teeReader{convertOut, fpcalcIn, nil}
	errbuf := bytes.NewBuffer(nil)
	fpcalcOut := bytes.NewBuffer(nil)
	fpcalc.Stdout = fpcalcOut
	fpcalc.Stderr = errbuf

	if err := fpcalc.Start(); err != nil {
		return "", 0, err
	}

	if err := convert.Start(); err != nil {
		_ = fpcalc.Process.Kill()
		return "", 0, err
	}

	buf := make([]byte, 1024)
	bytes := 0
	for {
		if err := ctx.Err(); err != nil {
			_ = fpcalc.Process.Kill()
			_ = convert.Process.Kill()
			return "", 0, err
		}

		n, err := tee.Read(buf)
		bytes += n
		if err != nil {
			if err == io.EOF {
				break
			}
			return "", 0, err
		}
	}
	dur := bytes / (4 * rate)

	_ = fpcalcIn.Close()
	if err := fpcalc.Wait(); err != nil {
		return "", dur, fmt.Errorf("%w: %s", err, errbuf.String())
	}

	_ = convertOut.Close()
	_ = convert.Wait()
	scan := bufio.NewScanner(fpcalcOut)
	scan.Split(bufio.ScanLines)
	for scan.Scan() {
		b := strings.TrimSpace(scan.Text())
		if strings.HasPrefix(b, pref) {
			return b[len(pref):], dur, nil
		}
	}

	if err := scan.Err(); err != nil {
		return "", dur, err
	}

	return "", dur, errors.New("no fingerprint found in output")
}

func (c *Client) LookupFile(ctx context.Context, file string) (*Response, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	return c.LookupReader(ctx, f)
}

func (c *Client) LookupReader(ctx context.Context, r io.Reader) (*Response, error) {
	fp, dur, err := c.Fingerprint(ctx, r)
	if err != nil {
		return nil, err
	}

	return c.Lookup(ctx, fp, dur)
}
