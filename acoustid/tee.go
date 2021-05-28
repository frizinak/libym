package acoustid

import "io"

// teeReader is identical to io.teeReader but doesn't fail after failed writes.
type teeReader struct {
	r    io.Reader
	w    io.Writer
	werr error
}

func (t *teeReader) Read(p []byte) (n int, err error) {
	n, err = t.r.Read(p)
	if n > 0 && t.werr == nil {
		if _, err := t.w.Write(p[:n]); err != nil {
			t.werr = err
		}
	}
	return
}
