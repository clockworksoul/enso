package core

import (
	"math"
	"testing"
	"time"
)

// --- helpers ---

func entryWithTemporal(t *testing.T, id ID, nt NodeType, tmp Temporal) Entry {
	t.Helper()
	enc := tmp.LastRefTime
	if enc.IsZero() {
		enc = time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
	}
	e, err := NewEntry(NewEntryParams{
		ID:          id,
		Type:        nt,
		Content:     "test entry",
		EncodedTime: enc,
		Confidence:  ConfHigh,
	})
	if err != nil {
		t.Fatalf("entryWithTemporal: %v", err)
	}
	e.Temporal = tmp // override after construction so tests control exact values
	return e
}

// baseTime is a fixed anchor used throughout tests.
var baseTime = time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)

// --- StrengthAt ---

func TestStrengthAt_Fresh(t *testing.T) {
	// At Δt = 0 (query right at LastRefTime), S should equal S_last.
	tmp := Temporal{
		LastRefTime: baseTime,
		SLast:       0.9,
		SFloor:      0.1,
		Lambda:      0.05,
		SCap:        1.0,
	}
	e := entryWithTemporal(t, "mem:2026-06-20-fresh", TypeFact, tmp)
	s := StrengthAt(e, baseTime)
	if math.Abs(s-0.9) > 1e-9 {
		t.Errorf("fresh: want S_last=0.9, got %v", s)
	}
}

func TestStrengthAt_Decayed(t *testing.T) {
	// After a long time, S should approach S_floor.
	tmp := Temporal{
		LastRefTime: baseTime,
		SLast:       1.0,
		SFloor:      0.1,
		Lambda:      0.05,
		SCap:        1.0,
	}
	e := entryWithTemporal(t, "mem:2026-06-20-decayed", TypeFact, tmp)
	// 10,000 hours is effectively ∞ for λ = 0.05.
	far := baseTime.Add(10_000 * time.Hour)
	s := StrengthAt(e, far)
	if math.Abs(s-0.1) > 1e-3 {
		t.Errorf("fully decayed: want ≈S_floor=0.1, got %v", s)
	}
}

func TestStrengthAt_HalfLife(t *testing.T) {
	// At Δt = ln(2)/λ, the gap (S_last − S_floor) should be halved.
	λ := 0.1
	sLast, sFloor := 0.8, 0.2
	gap := sLast - sFloor // 0.6
	halfLifeHours := math.Log(2) / λ
	tmp := Temporal{
		LastRefTime: baseTime,
		SLast:       sLast,
		SFloor:      sFloor,
		Lambda:      λ,
		SCap:        1.0,
	}
	e := entryWithTemporal(t, "mem:2026-06-20-halflife", TypeFact, tmp)
	halfLifeTime := baseTime.Add(time.Duration(halfLifeHours * float64(time.Hour)))
	s := StrengthAt(e, halfLifeTime)
	want := sFloor + gap/2
	if math.Abs(s-want) > 1e-9 {
		t.Errorf("half-life: want %v, got %v", want, s)
	}
}

func TestStrengthAt_ClockSkewGuard(t *testing.T) {
	// Negative Δt (query before LastRefTime) should be clamped to 0 → returns S_last.
	tmp := Temporal{
		LastRefTime: baseTime,
		SLast:       0.7,
		SFloor:      0.1,
		Lambda:      0.05,
		SCap:        1.0,
	}
	e := entryWithTemporal(t, "mem:2026-06-20-skew", TypeFact, tmp)
	before := baseTime.Add(-5 * time.Hour)
	s := StrengthAt(e, before)
	if math.Abs(s-0.7) > 1e-9 {
		t.Errorf("clock skew: want S_last=0.7 (clamped), got %v", s)
	}
}

func TestStrengthAt_ClampToSCap(t *testing.T) {
	// If SLast > SCap by float drift, result should be clamped to SCap.
	tmp := Temporal{
		LastRefTime: baseTime,
		SLast:       1.0000001, // slight float overshoot
		SFloor:      0.1,
		Lambda:      0.05,
		SCap:        1.0,
	}
	e := entryWithTemporal(t, "mem:2026-06-20-clamp", TypeFact, tmp)
	s := StrengthAt(e, baseTime)
	if s > 1.0 {
		t.Errorf("StrengthAt should clamp to SCap=1.0, got %v", s)
	}
}

func TestStrengthAt_TypeSpecificLambda(t *testing.T) {
	// A Task (high lambda) should decay faster than a Fact (low lambda).
	enc := baseTime
	task, _ := NewEntry(NewEntryParams{
		ID: "mem:2026-06-20-task", Type: TypeTask, Content: "do something",
		EncodedTime: enc, Confidence: ConfHigh,
	})
	fact, _ := NewEntry(NewEntryParams{
		ID: "mem:2026-06-20-fact", Type: TypeFact, Content: "sky is blue",
		EncodedTime: enc, Confidence: ConfHigh,
	})

	// One week later both should have decayed, but Task faster.
	later := baseTime.Add(7 * 24 * time.Hour)
	sTask := StrengthAt(task, later)
	sFact := StrengthAt(fact, later)
	if sTask >= sFact {
		t.Errorf("Task should decay faster than Fact: sTask=%v sFact=%v", sTask, sFact)
	}
}

// --- BumpOnRecall ---

func TestBumpOnRecall_NonDestructive(t *testing.T) {
	// BumpOnRecall must not mutate the original entry (value semantics).
	tmp := Temporal{
		LastRefTime: baseTime,
		SLast:       1.0,
		SFloor:      0.1,
		Lambda:      0.05,
		SCap:        1.0,
	}
	orig := entryWithTemporal(t, "mem:2026-06-20-bump-nd", TypeFact, tmp)
	origFloor := orig.Temporal.SFloor
	origLast := orig.Temporal.SLast
	origTime := orig.Temporal.LastRefTime

	params := RecallParams{Alpha: 0.10, Tau: 24}
	later := baseTime.Add(48 * time.Hour)
	_ = BumpOnRecall(orig, later, params)

	if orig.Temporal.SFloor != origFloor {
		t.Errorf("BumpOnRecall mutated SFloor: got %v, want %v", orig.Temporal.SFloor, origFloor)
	}
	if orig.Temporal.SLast != origLast {
		t.Errorf("BumpOnRecall mutated SLast")
	}
	if orig.Temporal.LastRefTime != origTime {
		t.Errorf("BumpOnRecall mutated LastRefTime")
	}
}

func TestBumpOnRecall_VividnessSpike(t *testing.T) {
	// After any recall, S_last should be reset to S_cap.
	tmp := Temporal{
		LastRefTime: baseTime,
		SLast:       0.3, // decayed well below S_cap
		SFloor:      0.1,
		Lambda:      0.05,
		SCap:        1.0,
	}
	e := entryWithTemporal(t, "mem:2026-06-20-spike", TypeFact, tmp)
	params := RecallParams{Alpha: 0.10, Tau: 24}
	bumped := BumpOnRecall(e, baseTime.Add(48*time.Hour), params)

	if bumped.Temporal.SLast != bumped.Temporal.SCap {
		t.Errorf("SLast after bump should equal SCap=%v, got %v", bumped.Temporal.SCap, bumped.Temporal.SLast)
	}
}

func TestBumpOnRecall_FloorRises(t *testing.T) {
	// A bump at well-spaced recall should raise S_floor strictly.
	tmp := Temporal{
		LastRefTime: baseTime,
		SLast:       1.0,
		SFloor:      0.1,
		Lambda:      0.05,
		SCap:        1.0,
	}
	e := entryWithTemporal(t, "mem:2026-06-20-floor-rise", TypeFact, tmp)
	params := RecallParams{Alpha: 0.10, Tau: 24}
	bumped := BumpOnRecall(e, baseTime.Add(48*time.Hour), params)

	if bumped.Temporal.SFloor <= tmp.SFloor {
		t.Errorf("SFloor should have risen: was %v, got %v", tmp.SFloor, bumped.Temporal.SFloor)
	}
}

func TestBumpOnRecall_SpacingEffect_ImmediateBump(t *testing.T) {
	// A recall at Δt ≈ 0 should produce essentially no floor consolidation
	// (spacing multiplier ≈ 0 → α_eff ≈ 0).
	tmp := Temporal{
		LastRefTime: baseTime,
		SLast:       1.0,
		SFloor:      0.1,
		Lambda:      0.05,
		SCap:        1.0,
	}
	e := entryWithTemporal(t, "mem:2026-06-20-immediate", TypeFact, tmp)
	params := RecallParams{Alpha: 0.10, Tau: 24}
	// One second later: Δt ≈ 0 hours → multiplier ≈ 0.
	bumped := BumpOnRecall(e, baseTime.Add(time.Second), params)

	delta := bumped.Temporal.SFloor - tmp.SFloor
	if delta > 0.001 {
		t.Errorf("immediate recall should barely raise floor; got delta=%v", delta)
	}
}

func TestBumpOnRecall_SpacingEffect_WellSpaced(t *testing.T) {
	// A recall at Δt ≫ Tau should produce a bump close to Alpha * headroom.
	sFloor := 0.1
	sCap := 1.0
	headroom := sCap - sFloor // 0.9
	alpha := 0.10
	tmp := Temporal{
		LastRefTime: baseTime,
		SLast:       sCap,
		SFloor:      sFloor,
		Lambda:      0.05,
		SCap:        sCap,
	}
	e := entryWithTemporal(t, "mem:2026-06-20-spaced", TypeFact, tmp)
	params := RecallParams{Alpha: alpha, Tau: 24}
	// 10 * Tau ≈ 10 days: f(Δt) ≈ 1 − e^(−10) ≈ 0.99995.
	bumped := BumpOnRecall(e, baseTime.Add(10*24*time.Hour), params)

	delta := bumped.Temporal.SFloor - sFloor
	wantApprox := alpha * headroom // ~0.09
	if math.Abs(delta-wantApprox) > 0.002 {
		t.Errorf("well-spaced recall: want floor delta ≈ %v, got %v", wantApprox, delta)
	}
}

func TestBumpOnRecall_LastRefTimeUpdated(t *testing.T) {
	tmp := Temporal{
		LastRefTime: baseTime,
		SLast:       1.0,
		SFloor:      0.1,
		Lambda:      0.05,
		SCap:        1.0,
	}
	e := entryWithTemporal(t, "mem:2026-06-20-time-update", TypeFact, tmp)
	recallTime := baseTime.Add(48 * time.Hour)
	bumped := BumpOnRecall(e, recallTime, RecallParams{Alpha: 0.10, Tau: 24})

	if !bumped.Temporal.LastRefTime.Equal(recallTime) {
		t.Errorf("LastRefTime not updated: want %v, got %v", recallTime, bumped.Temporal.LastRefTime)
	}
}

func TestBumpOnRecall_ClockSkewGuard(t *testing.T) {
	// Negative Δt should be clamped to 0: no floor change, spike still fires.
	tmp := Temporal{
		LastRefTime: baseTime,
		SLast:       1.0,
		SFloor:      0.2,
		Lambda:      0.05,
		SCap:        1.0,
	}
	e := entryWithTemporal(t, "mem:2026-06-20-bump-skew", TypeFact, tmp)
	before := baseTime.Add(-1 * time.Hour)
	bumped := BumpOnRecall(e, before, RecallParams{Alpha: 0.10, Tau: 24})

	// Floor should be essentially unchanged (Δt clamped to 0 → f(0) = 0).
	if math.Abs(bumped.Temporal.SFloor-tmp.SFloor) > 1e-9 {
		t.Errorf("clock skew: floor should not change, got delta=%v", bumped.Temporal.SFloor-tmp.SFloor)
	}
}

// --- DefaultRecallParams ---

func TestDefaultRecallParams_AllTypes(t *testing.T) {
	// Every valid NodeType should return a RecallParams with positive Alpha and Tau.
	for nt := range ValidNodeTypes {
		p := DefaultRecallParams(nt)
		if p.Alpha <= 0 || p.Alpha > 1 {
			t.Errorf("%s: Alpha must be in (0,1], got %v", nt, p.Alpha)
		}
		if p.Tau <= 0 {
			t.Errorf("%s: Tau must be positive, got %v", nt, p.Tau)
		}
	}
}

func TestDefaultRecallParams_TaskTighterTau(t *testing.T) {
	// Task (volatile) should have a shorter Tau than a stable Fact.
	taskP := DefaultRecallParams(TypeTask)
	factP := DefaultRecallParams(TypeFact)
	if taskP.Tau >= factP.Tau {
		t.Errorf("Task Tau (%v) should be shorter than Fact Tau (%v)", taskP.Tau, factP.Tau)
	}
}

// --- Rank ---

func TestRank_OrderByStrength(t *testing.T) {
	// Three entries: fresh, semi-decayed, very-old. Rank should return fresh first.
	newTmp := func(last float64, hoursAgo float64) Temporal {
		return Temporal{
			LastRefTime: baseTime.Add(-time.Duration(hoursAgo * float64(time.Hour))),
			SLast:       last,
			SFloor:      0.1,
			Lambda:      0.05,
			SCap:        1.0,
		}
	}
	fresh := entryWithTemporal(t, "mem:2026-06-20-fresh2", TypeFact, newTmp(1.0, 0))
	mid := entryWithTemporal(t, "mem:2026-06-20-mid", TypeFact, newTmp(1.0, 48))
	old := entryWithTemporal(t, "mem:2026-06-20-old", TypeFact, newTmp(1.0, 720))

	ranked := Rank([]Entry{old, mid, fresh}, baseTime) // deliberately shuffled order
	if ranked[0].Entry.ID != fresh.ID {
		t.Errorf("rank[0] should be fresh, got %q (strength=%v)", ranked[0].Entry.ID, ranked[0].Strength)
	}
	if ranked[1].Entry.ID != mid.ID {
		t.Errorf("rank[1] should be mid, got %q", ranked[1].Entry.ID)
	}
	if ranked[2].Entry.ID != old.ID {
		t.Errorf("rank[2] should be old, got %q", ranked[2].Entry.ID)
	}
}

func TestRank_StrengthFieldPopulated(t *testing.T) {
	e := entryWithTemporal(t, "mem:2026-06-20-strength", TypeFact, Temporal{
		LastRefTime: baseTime,
		SLast:       0.8,
		SFloor:      0.1,
		Lambda:      0.05,
		SCap:        1.0,
	})
	ranked := Rank([]Entry{e}, baseTime)
	if len(ranked) != 1 {
		t.Fatalf("want 1 ranked entry, got %d", len(ranked))
	}
	if math.Abs(ranked[0].Strength-0.8) > 1e-9 {
		t.Errorf("Strength field not populated correctly: got %v, want 0.8", ranked[0].Strength)
	}
}

func TestRank_Empty(t *testing.T) {
	ranked := Rank(nil, baseTime)
	if len(ranked) != 0 {
		t.Errorf("Rank(nil) should return empty slice, got %v", ranked)
	}
}

func TestRank_StableSort(t *testing.T) {
	// Equal-strength entries should preserve input order.
	tmp := Temporal{
		LastRefTime: baseTime,
		SLast:       0.5,
		SFloor:      0.5, // S_floor == S_last → strength constant regardless of decay
		Lambda:      0.05,
		SCap:        1.0,
	}
	a := entryWithTemporal(t, "mem:2026-06-20-stable-a", TypeFact, tmp)
	b := entryWithTemporal(t, "mem:2026-06-20-stable-b", TypeFact, tmp)
	c := entryWithTemporal(t, "mem:2026-06-20-stable-c", TypeFact, tmp)

	ranked := Rank([]Entry{a, b, c}, baseTime.Add(100*time.Hour))
	if ranked[0].Entry.ID != a.ID || ranked[1].Entry.ID != b.ID || ranked[2].Entry.ID != c.ID {
		t.Errorf("equal-strength entries should preserve order: got %q %q %q",
			ranked[0].Entry.ID, ranked[1].Entry.ID, ranked[2].Entry.ID)
	}
}

func TestRank_HighFloorBeatsDecayedHighLast(t *testing.T) {
	// A high-floor, lightly-decayed entry should rank above a high-S_last entry
	// that has significantly decayed.
	wellConsolidated := entryWithTemporal(t, "mem:2026-06-20-consolidated", TypeFact, Temporal{
		LastRefTime: baseTime.Add(-200 * time.Hour), // 200h ago
		SLast:       0.8,
		SFloor:      0.75, // high floor — well consolidated
		Lambda:      0.05,
		SCap:        1.0,
	})
	recentButForgetting := entryWithTemporal(t, "mem:2026-06-20-forgetting", TypeFact, Temporal{
		LastRefTime: baseTime.Add(-200 * time.Hour), // same age
		SLast:       0.8,
		SFloor:      0.05, // low floor — will decay near zero
		Lambda:      0.05,
		SCap:        1.0,
	})
	ranked := Rank([]Entry{recentButForgetting, wellConsolidated}, baseTime)
	if ranked[0].Entry.ID != wellConsolidated.ID {
		t.Errorf("well-consolidated entry should rank higher; got %q (strength=%v) over %q (strength=%v)",
			ranked[0].Entry.ID, ranked[0].Strength, ranked[1].Entry.ID, ranked[1].Strength)
	}
}
