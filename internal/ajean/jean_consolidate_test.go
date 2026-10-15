package ajean

import (
	"testing"
	"time"
)

func TestJeanSleepWindow(t *testing.T) {
	testHome(t)
	rememberUserTZ("Europe/Paris")
	paris := loc("Europe/Paris")
	at := func(h, m int) time.Time { return time.Date(2026, 10, 9, h, m, 0, 0, paris) }
	for _, c := range []struct {
		now  time.Time
		want time.Duration
	}{
		{at(3, 0), 0},
		{at(4, 59), 0},
		{at(13, 30), 13*time.Hour + 30*time.Minute},
		{at(5, 0), 22 * time.Hour},
		{at(2, 30), 30 * time.Minute},
	} {
		if got := jeanUntilSleepWindow(c.now); got != c.want {
			t.Errorf("%s : %v, attendu %v", c.now.Format("15:04"), got, c.want)
		}
	}
}
