package youtube

import (
	"testing"
)

func TestParse(t *testing.T) {
	id := "videoid"
	us := []string{
		"https://www.youtube.com/watch?v=videoid&feature=featured",
		"https://www.youtube.com/watch?v=videoid",
		"http://www.youtube.com/watch?v=videoid",
		"//www.youtube.com/watch?v=videoid",
		"www.youtube.com/watch?v=videoid",
		"https://youtube.com/watch?v=videoid",
		"http://youtube.com/watch?v=videoid",
		"//youtube.com/watch?v=videoid",
		"youtube.com/watch?v=videoid",
		"https://m.youtube.com/watch?v=videoid",
		"http://m.youtube.com/watch?v=videoid",
		"//m.youtube.com/watch?v=videoid",
		"m.youtube.com/watch?v=videoid",
		"https://www.youtube.com/v/videoid?fs=1&hl=en_US",
		"http://www.youtube.com/v/videoid?fs=1&hl=en_US",
		"//www.youtube.com/v/videoid?fs=1&hl=en_US",
		"www.youtube.com/v/videoid?fs=1&hl=en_US",
		"youtube.com/v/videoid?fs=1&hl=en_US",
		"https://www.youtube.com/embed/videoid?autoplay=1",
		"https://www.youtube.com/embed/videoid",
		"http://www.youtube.com/embed/videoid",
		"//www.youtube.com/embed/videoid",
		"www.youtube.com/embed/videoid",
		"https://youtube.com/embed/videoid",
		"http://youtube.com/embed/videoid",
		"//youtube.com/embed/videoid",
		"youtube.com/embed/videoid",
		"https://youtu.be/videoid?t=120",
		"https://youtu.be/videoid",
		"http://youtu.be/videoid",
		"//youtu.be/videoid",
		"youtu.be/videoid",
		"https://www.youtube.com/HamdiKickProduction?v=videoid",
	}

	for _, u := range us {
		v, err := FromURL(u, "")
		if err != nil {
			t.Error(err)
		}

		vid := v.ID()
		if vid != id {
			t.Errorf("invalid id: %s: %s", u, vid)
		}
	}
}
