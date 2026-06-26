package ratelimit

import "testing"

func TestTracker_UnknownBeforeAnyObservation(t *testing.T) {
	var tr Tracker
	if _, known := tr.Limit(); known {
		t.Error("expected known=false before any Observe call")
	}
}

func TestTracker_ObserveThenLimit(t *testing.T) {
	var tr Tracker
	tr.Observe(600)
	limit, known := tr.Limit()
	if !known || limit != 600 {
		t.Errorf("Limit() = (%d, %v), want (600, true)", limit, known)
	}
}

func TestTracker_NonPositiveObservationsIgnored(t *testing.T) {
	var tr Tracker
	tr.Observe(600)
	tr.Observe(0)
	tr.Observe(-5)
	limit, known := tr.Limit()
	if !known || limit != 600 {
		t.Errorf("Limit() = (%d, %v), want (600, true) -- non-positive observations must not overwrite", limit, known)
	}
}

func TestTracker_LatestObservationWins(t *testing.T) {
	var tr Tracker
	tr.Observe(600)
	tr.Observe(300)
	limit, known := tr.Limit()
	if !known || limit != 300 {
		t.Errorf("Limit() = (%d, %v), want (300, true) -- the API's own documentation warns this isn't fixed", limit, known)
	}
}
