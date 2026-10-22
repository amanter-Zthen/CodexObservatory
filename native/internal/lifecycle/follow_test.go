package lifecycle

import (
	"testing"
	"time"
)

func TestFollowWholeAppThroughRepeatedStartsAndGracefulExits(t *testing.T) {
	t0 := time.Now()
	f := Follower{Grace: 3 * time.Second}
	steps := []struct {
		present, known bool
		seconds        int
		want           Action
	}{{false, true, 0, None}, {true, true, 1, Start}, {true, true, 2, None}, {false, true, 3, None}, {true, true, 4, None}, {false, true, 5, None}, {false, true, 7, None}, {false, true, 8, Stop}, {false, true, 9, None}, {true, true, 10, Start}}
	for _, s := range steps {
		if a := f.Update(s.present, s.known, t0.Add(time.Duration(s.seconds)*time.Second)); a != s.want {
			t.Fatalf("at=%d action=%s wanted=%s", s.seconds, a, s.want)
		}
	}
}
func TestTransientProcessInspectionFailureKeepsCompanionAndResetsExitGrace(t *testing.T) {
	t0 := time.Now()
	f := Follower{Grace: 3 * time.Second}
	f.Update(true, true, t0)
	f.Update(false, true, t0.Add(time.Second))
	if a := f.Update(false, false, t0.Add(10*time.Second)); a != None || !f.Active {
		t.Fatal("unknown inspection closed companion")
	}
	if a := f.Update(false, true, t0.Add(11*time.Second)); a != None {
		t.Fatal("grace not restarted")
	}
	if a := f.Update(false, true, t0.Add(14*time.Second)); a != Stop {
		t.Fatal("confirmed app exit not handled")
	}
}
