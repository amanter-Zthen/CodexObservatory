package floatui

import "testing"

func TestOnlyFreshSuccessfulIntervalCanPulseAfterCompletion(t *testing.T) {
	e := engine()
	v := event(ResponseFinished, 1, "one")
	v.OutputTokens = 100
	v.ElapsedSeconds = 5
	v.TokenRate = 20
	apply(e, v)
	if row := one(t, e, 1); !row.SpeedValid || !row.SpeedFresh || row.Outputting {
		t.Fatal(row)
	}
	v = event(ResponseMetadata, 4, "one")
	v.Reported = "wanted"
	apply(e, v)
	if row := one(t, e, 4); !row.SpeedValid || row.SpeedFresh {
		t.Fatal("metadata resurrected rate pulse", row)
	}
	v = event(ResponseFailed, 5, "one")
	apply(e, v)
	if row := one(t, e, 5); row.SpeedValid || row.SpeedFresh {
		t.Fatal("failed response kept completed rate pulse", row)
	}
	e = engine()
	v = event(ResponseFinished, 1, "one")
	v.OutputTokens = 100
	v.ElapsedSeconds = 5
	v.TokenRate = 999
	apply(e, v)
	if one(t, e, 1).SpeedValid {
		t.Fatal("inconsistent interval accepted")
	}
}
