package collection

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"time"
)

func Download(w io.Writer, src *url.URL) error {
	req, err := http.NewRequest("GET", src.String(), nil)
	if err != nil {
		return err
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	_, err = io.Copy(w, res.Body)
	return err
}

func DownloadAudio(w io.Writer, src *url.URL) error {
	ff := exec.Command(
		"ffmpeg",
		"-i",
		"-",
		"-vn",
		"-f",
		"adts",
		"-",
	)
	pipe, err := ff.StdinPipe()
	if err != nil {
		return err
	}
	bufe := bytes.NewBuffer(nil)
	ff.Stdout = w
	ff.Stderr = bufe
	errs := make(chan error, 1)
	go func() {
		if err := ff.Run(); err != nil {
			errs <- fmt.Errorf("%w: %s", err, bufe.String())
			return
		}
		errs <- nil
	}()

	err = Download(pipe, src)
	pipe.Close()
	if err != nil {
		return err
	}

	return <-errs
}

func TempFile(file string) string {
	stamp := strconv.FormatInt(time.Now().UnixNano(), 36)
	rnd := make([]byte, 32)
	_, err := io.ReadFull(rand.Reader, rnd)
	if err != nil {
		panic(err)
	}

	return fmt.Sprintf(
		"%s.%s-%s.tmp",
		file,
		stamp,
		base64.RawURLEncoding.EncodeToString(rnd),
	)
}
