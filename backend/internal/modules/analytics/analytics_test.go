package analytics

import (
	"net/http/httptest"
	"testing"
)

func TestParseParamsDefaults(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/v1/admin/analytics/overview", nil)
	p, err := ParseParams(r, true)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p.Granularity != GranularityDay || p.Page != 1 || p.PerPage != DefaultLimit {
		t.Fatalf("defaults wrong: %+v", p)
	}
	if p.DormantDays != DefaultDormantDays || p.ChurnDropPct != DefaultChurnDropPct || p.StuckMinutes != DefaultStuckMinutes {
		t.Fatalf("threshold defaults wrong: %+v", p)
	}
	if p.VolumeWindowDays != DefaultVolumeWindowDays {
		t.Fatalf("volume window default wrong: %+v", p)
	}
	if !p.To.After(p.From) {
		t.Fatal("to must be after from")
	}
}

func TestParseParamsVolumeDays(t *testing.T) {
	r := httptest.NewRequest("GET", "/?volume_days=30", nil)
	p, err := ParseParams(r, true)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p.VolumeWindowDays != 30 {
		t.Fatalf("volume_days wrong: %+v", p.VolumeWindowDays)
	}
	r2 := httptest.NewRequest("GET", "/?volume_days=banana", nil)
	p2, err := ParseParams(r2, true)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p2.VolumeWindowDays != DefaultVolumeWindowDays {
		t.Fatalf("bad volume_days must fall back to default: %+v", p2.VolumeWindowDays)
	}
}

func TestParseParamsRejects(t *testing.T) {
	for _, url := range []string{
		"/?from=2026-13-99",
		"/?from=2026-09-28&to=2026-09-01",
		"/?granularity=fortnight",
		"/?from=2020-01-01&to=2026-09-28",
	} {
		r := httptest.NewRequest("GET", url, nil)
		if _, err := ParseParams(r, true); err == nil {
			t.Fatalf("expected error for %s", url)
		}
	}
}

func TestParseParamsOrgFilter(t *testing.T) {
	r := httptest.NewRequest("GET", "/?org_id=abc&currency=tzs&provider=sonicpesa&page=2&per_page=10", nil)
	p, err := ParseParams(r, true)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(p.OrgIDs) != 1 || p.OrgIDs[0] != "abc" {
		t.Fatalf("org filter wrong: %+v", p.OrgIDs)
	}
	if p.Currency != "TZS" || p.Provider != "sonicpesa" || p.Page != 2 || p.PerPage != 10 {
		t.Fatalf("filters wrong: %+v", p)
	}
	// Disallowed: org filter ignored.
	r2 := httptest.NewRequest("GET", "/?org_id=abc", nil)
	p2, err := ParseParams(r2, false)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(p2.OrgIDs) != 0 {
		t.Fatalf("org filter must be ignored when disallowed: %+v", p2.OrgIDs)
	}
}

func TestPreviousPeriod(t *testing.T) {
	r := httptest.NewRequest("GET", "/?from=2026-09-01&to=2026-10-01", nil)
	p, err := ParseParams(r, true)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	prevFrom, prevTo := p.PreviousPeriod()
	if !prevTo.Equal(p.From) || prevTo.Sub(prevFrom) != p.To.Sub(p.From) {
		t.Fatalf("previous period wrong: %v %v", prevFrom, prevTo)
	}
}
