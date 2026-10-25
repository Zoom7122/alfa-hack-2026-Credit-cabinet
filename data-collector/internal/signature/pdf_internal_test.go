package signature

import (
	"testing"
	"time"
)

func TestParsePDFDate(t *testing.T) {
	msk := time.FixedZone("", 3*3600)
	for in, want := range map[string]time.Time{
		"D:20261022083015+03'00'": time.Date(2026, 10, 22, 8, 30, 15, 0, msk),
		"D:20261022083015+03'00":  time.Date(2026, 10, 22, 8, 30, 15, 0, msk),
		"D:20261022083015Z":       time.Date(2026, 10, 22, 8, 30, 15, 0, time.UTC),
		"D:20261022083015":        time.Date(2026, 10, 22, 8, 30, 15, 0, time.UTC),
		"D:202610220830":          time.Date(2026, 10, 22, 8, 30, 0, 0, time.UTC),
		"D:2026102208":            time.Date(2026, 10, 22, 8, 0, 0, 0, time.UTC),
		"D:20261022083015-05'30'": time.Date(2026, 10, 22, 8, 30, 15, 0, time.FixedZone("", -(5*3600+30*60))),
	} {
		got, ok := parsePDFDate(in)
		if !ok || !got.Equal(want) {
			t.Errorf("parsePDFDate(%q) = %v, %v; want %v", in, got, ok, want)
		}
	}
	for _, in := range []string{"D:20261022", "D:202610220", "D:20261322083015", "D:20261022083015+3", "garbage", ""} {
		if got, ok := parsePDFDate(in); ok {
			t.Errorf("parsePDFDate(%q) = %v, want failure", in, got)
		}
	}
}
