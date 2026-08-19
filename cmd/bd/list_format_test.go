//go:build cgo

package main

import (
	"testing"
	"time"
)

// zonesUnderTest supplies the reference clock's location through the now
// argument rather than reading the machine's, so every expectation below holds
// on any host and in CI. A test that took its zone from the environment would
// pass on a UTC runner against the very bug this guards.
var zonesUnderTest = []struct {
	name string
	loc  *time.Location
}{
	{"UTC", time.UTC},
	{"NewYork", time.FixedZone("EDT", -4*60*60)},
	{"Kiritimati", time.FixedZone("LINT", 14*60*60)},
}

// TestParseAuditTimeAtAnchorsBareDatesToUTC is the fix for #5823: a bare date on
// an audit bound names the UTC day, so the same command selects the same rows
// whatever the host's timezone is.
func TestParseAuditTimeAtAnchorsBareDatesToUTC(t *testing.T) {
	for _, zone := range zonesUnderTest {
		now := time.Date(2026, 8, 16, 20, 13, 18, 0, zone.loc)
		t.Run(zone.name, func(t *testing.T) {
			got, err := parseAuditTimeAt("2026-08-17", now)
			if err != nil {
				t.Fatalf("parseAuditTimeAt: %v", err)
			}
			want := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
			if !got.Equal(want) {
				t.Errorf("bare audit date resolved to %v, want %v", got, want)
			}
		})
	}
}

// TestParseAuditTimeAtRoundTripsAJSONTimestamp is the reported failure. A UTC
// created_at is read out of --json, reduced to its calendar date, and reused as
// a bound. The record it came from must fall on the same side of that bound on
// every host.
func TestParseAuditTimeAtRoundTripsAJSONTimestamp(t *testing.T) {
	createdAt := time.Date(2026, 8, 16, 20, 13, 18, 0, time.UTC)

	for _, zone := range zonesUnderTest {
		now := time.Date(2026, 8, 17, 12, 0, 0, 0, zone.loc)
		t.Run(zone.name, func(t *testing.T) {
			nextDay, err := parseAuditTimeAt("2026-08-17", now)
			if err != nil {
				t.Fatalf("parseAuditTimeAt: %v", err)
			}
			if !createdAt.Before(nextDay) {
				t.Errorf("--created-after 2026-08-17 bound %v does not exclude a bead created %v", nextDay, createdAt)
			}

			sameDay, err := parseAuditTimeAt("2026-08-16", now)
			if err != nil {
				t.Fatalf("parseAuditTimeAt: %v", err)
			}
			if !createdAt.After(sameDay) {
				t.Errorf("--created-after 2026-08-16 bound %v does not include a bead created %v", sameDay, createdAt)
			}
		})
	}
}

// TestParseAuditTimeAtLeavesEverythingElseAlone pins the deliberate narrowness
// of the fix. Only the bare date moves. Relative expressions describe a position
// relative to wherever the caller is, and anchoring them to a UTC clock moves
// "next monday" by up to a week once UTC has crossed into the following day.
// Timezone-less datetimes keep their conventional local wall-clock meaning.
func TestParseAuditTimeAtLeavesEverythingElseAlone(t *testing.T) {
	edt := time.FixedZone("EDT", -4*60*60)
	// Sunday 20:00 EDT is already Monday 00:00 UTC.
	now := time.Date(2026, 8, 16, 20, 0, 0, 0, edt)

	for _, tt := range []struct {
		input string
		want  time.Time
	}{
		// Calendar-relative: must follow the caller's calendar, not UTC's.
		{"next monday", time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)},
		{"monday", time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)},
		{"tomorrow", time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)},
		{"in 3 days", time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)},
		// Compact durations walk the caller's calendar via AddDate.
		{"+1d", time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)},
		{"-6h", time.Date(2026, 8, 16, 18, 0, 0, 0, time.UTC)},
		// Timezone-less datetime stays local wall clock: 09:30 EDT is 13:30Z.
		{"2026-08-17T09:30:00", time.Date(2026, 8, 17, 13, 30, 0, 0, time.UTC)},
		{"2026-08-17 09:30:00", time.Date(2026, 8, 17, 13, 30, 0, 0, time.UTC)},
		// Explicit offsets are honored exactly as written.
		{"2026-08-17T09:30:00Z", time.Date(2026, 8, 17, 9, 30, 0, 0, time.UTC)},
		{"2026-08-17T09:30:00-06:00", time.Date(2026, 8, 17, 15, 30, 0, 0, time.UTC)},
	} {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseAuditTimeAt(tt.input, now)
			if err != nil {
				t.Fatalf("parseAuditTimeAt(%q): %v", tt.input, err)
			}
			if !got.Equal(tt.want) {
				t.Errorf("parseAuditTimeAt(%q) = %v, want %v (only the bare date should move to UTC)",
					tt.input, got, tt.want)
			}
		})
	}
}

// TestParseScheduleTimeAtStaysLocal keeps the due and defer bounds on the same
// clock as the writes and the display. bd show renders due_at and defer_until
// through .Local(), and --due/--defer read them locally, so a bound naming the
// date the user typed into --due has to select the issue it created. Anchoring
// these to UTC would put them up to fourteen hours off that instant.
func TestParseScheduleTimeAtStaysLocal(t *testing.T) {
	for _, zone := range zonesUnderTest {
		now := time.Date(2026, 8, 16, 20, 13, 18, 0, zone.loc)
		t.Run(zone.name, func(t *testing.T) {
			got, err := parseScheduleTimeAt("2026-08-20", now)
			if err != nil {
				t.Fatalf("parseScheduleTimeAt: %v", err)
			}
			want := time.Date(2026, 8, 20, 0, 0, 0, 0, zone.loc).UTC()
			if !got.Equal(want) {
				t.Errorf("schedule bound resolved to %v, want local midnight %v", got, want)
			}
		})
	}
}

// TestScheduleBoundMatchesWhatDueWrote is the concrete reason the schedule
// bounds stay local. On a UTC+14 host, --due 2026-08-20 stores 2026-08-19T10:00Z
// and bd show still prints "Due: 2026-08-20". A UTC-anchored --due-after bound
// naming that same date would sit fourteen hours later and drop the issue.
func TestScheduleBoundMatchesWhatDueWrote(t *testing.T) {
	lint := time.FixedZone("LINT", 14*60*60)
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, lint)

	// What --due 2026-08-20 stores, per the write path.
	stored, err := parseScheduleTimeAt("2026-08-20", now)
	if err != nil {
		t.Fatalf("write parse: %v", err)
	}

	bound, err := parseScheduleTimeAt("2026-08-19", now)
	if err != nil {
		t.Fatalf("bound parse: %v", err)
	}
	if !stored.After(bound) {
		t.Errorf("--due-after 2026-08-19 bound %v does not select an issue due 2026-08-20 (%v)", bound, stored)
	}

	auditBound, err := parseAuditTimeAt("2026-08-20", now)
	if err != nil {
		t.Fatalf("audit parse: %v", err)
	}
	if stored.Equal(auditBound) {
		t.Fatal("audit and schedule bounds coincide here, so this test proves nothing")
	}
}

// TestListDateFlagsAreWiredToTheRightParser is the wiring guard. The contracts
// above are only worth anything if each flag reaches the parser matching its
// column, so this drives the real listCmd and compares each bound against the
// parser it should have used. Routing a due or defer bound through the audit
// parser, or an audit bound through the schedule parser, fails here.
//
// The two parsers coincide when the host runs in UTC, so this test discriminates
// only off UTC. The semantics themselves are pinned host-independently above;
// this one exists to catch a miswired flag.
func TestListDateFlagsAreWiredToTheRightParser(t *testing.T) {
	names := []string{"created-after", "updated-after", "closed-after", "due-after", "defer-after"}
	flags := listCmd.Flags()
	for _, name := range names {
		if err := flags.Set(name, "2026-08-20"); err != nil {
			t.Fatalf("set --%s: %v", name, err)
		}
	}
	t.Cleanup(func() {
		for _, name := range names {
			_ = flags.Set(name, "")
		}
	})

	wantAudit, err := parseAuditTimeFlag("2026-08-20")
	if err != nil {
		t.Fatalf("parseAuditTimeFlag: %v", err)
	}
	wantSchedule, err := parseScheduleTimeFlag("2026-08-20")
	if err != nil {
		t.Fatalf("parseScheduleTimeFlag: %v", err)
	}

	in, err := gatherListInput(listCmd)
	if err != nil {
		t.Fatalf("gatherListInput: %v", err)
	}

	for _, tt := range []struct {
		flag   string
		got    *time.Time
		want   time.Time
		parser string
	}{
		{"created-after", in.CreatedAfter, wantAudit, "audit"},
		{"updated-after", in.UpdatedAfter, wantAudit, "audit"},
		{"closed-after", in.ClosedAfter, wantAudit, "audit"},
		{"due-after", in.DueAfter, wantSchedule, "schedule"},
		{"defer-after", in.DeferAfter, wantSchedule, "schedule"},
	} {
		if tt.got == nil {
			t.Errorf("--%s produced no bound", tt.flag)
			continue
		}
		if !tt.got.Equal(tt.want) {
			t.Errorf("--%s bound = %v, want %v (should use the %s parser)", tt.flag, *tt.got, tt.want, tt.parser)
		}
	}
}
