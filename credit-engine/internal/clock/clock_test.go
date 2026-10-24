package clock_test

import (
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/clock"
)

func TestUTCClocks(t *testing.T) {
	local := time.Date(2026, 10, 23, 15, 0, 0, 0, time.FixedZone("Moscow", 3*60*60))
	fixed := clock.Fixed{Time: local}
	if got := fixed.Now(); !got.Equal(local) || got.Location() != time.UTC || got.Hour() != 12 {
		t.Fatalf("unexpected fixed time: %v", got)
	}
	before := time.Now()
	got := (clock.System{}).Now()
	after := time.Now()
	if got.Before(before) || got.After(after) || got.Location() != time.UTC {
		t.Fatalf("unexpected system time: %v", got)
	}
}
