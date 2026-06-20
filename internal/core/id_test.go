package core

import (
	"testing"
	"time"
)

func TestSlugify(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Omega Lite MVP", "omega-lite-mvp"},
		{"  leading/trailing  ", "leadingtrailing"},
		{"Already-Kebab-Case", "already-kebab-case"},
		{"multiple   spaces", "multiple-spaces"},
		{"punctuation!@#$%^&*()", "punctuation"},
		{"mixed CASE 123", "mixed-case-123"},
		{"--dashes--everywhere--", "dashes-everywhere"},
		{"under_score becomes nothing", "underscore-becomes-nothing"},
		{"", ""},
		{"!@#$", ""},
		{"café build", "caf-build"}, // non-ascii stripped, not transliterated
	}
	for _, c := range cases {
		if got := Slugify(c.in); got != c.want {
			t.Errorf("Slugify(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNewID(t *testing.T) {
	date := time.Date(2026, 6, 20, 14, 30, 0, 0, time.UTC)
	id, err := NewID(date, "Hexagonal Architecture")
	if err != nil {
		t.Fatalf("NewID: unexpected error: %v", err)
	}
	if want := ID("mem:2026-06-20-hexagonal-architecture"); id != want {
		t.Errorf("NewID = %q, want %q", id, want)
	}
	if err := id.Validate(); err != nil {
		t.Errorf("constructed id should validate: %v", err)
	}
}

func TestNewID_EmptySlugFails(t *testing.T) {
	date := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
	if _, err := NewID(date, "!@#$%"); err == nil {
		t.Fatal("NewID with unslugifiable label should error")
	}
}

func TestID_Validate(t *testing.T) {
	valid := []ID{
		"mem:2026-06-20-foo",
		"mem:2026-06-20-foo-bar-baz",
		"mem:2000-01-01-x",
		"mem:2026-12-31-abc123",
	}
	for _, id := range valid {
		if err := id.Validate(); err != nil {
			t.Errorf("Validate(%q) should pass, got %v", id, err)
		}
	}

	invalid := []ID{
		"",                        // empty
		"2026-06-20-foo",          // missing prefix
		"mem:2026-6-20-foo",       // unpadded month
		"mem:2026-06-20-",         // empty slug
		"mem:2026-06-20",          // no slug at all
		"mem:2026-06-20-Foo",      // uppercase in slug
		"mem:2026-06-20-foo_bar",  // underscore not allowed
		"mem:2026-13-20-foo",      // invalid month
		"mem:2026-02-30-foo",      // invalid day
		"mem:2026-06-20-foo--bar", // double hyphen in slug
		"mem:2026-06-20-foo bar",  // space in slug
		"mem:notadate-foo",        // bad date shape
	}
	for _, id := range invalid {
		if err := id.Validate(); err == nil {
			t.Errorf("Validate(%q) should fail, got nil", id)
		}
	}
}

func TestID_DateAndSlug(t *testing.T) {
	id := ID("mem:2026-06-20-foo-bar")
	d, err := id.Date()
	if err != nil {
		t.Fatalf("Date: %v", err)
	}
	if !d.Equal(time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("Date = %v, want 2026-06-20", d)
	}
	slug, err := id.Slug()
	if err != nil {
		t.Fatalf("Slug: %v", err)
	}
	if slug != "foo-bar" {
		t.Errorf("Slug = %q, want foo-bar", slug)
	}
}
