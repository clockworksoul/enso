package core

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// ID is a memory node identifier of the form `mem:<YYYY-MM-DD>-<slug>`
// (tech spec §3.2). It is the stable join key the graph will rely on, so:
//   - it is stable forever and never reused,
//   - collisions are bugs,
//   - the slug is a kebab-case token.
//
// ID is a distinct type (not a bare string) so the domain can enforce the
// format at the type boundary rather than trusting callers.
type ID string

const idPrefix = "mem:"

// idPattern matches `mem:YYYY-MM-DD-<slug>` where slug is one or more
// kebab-case tokens of lowercase alphanumerics. The date portion is validated
// separately (regex alone can't reject e.g. month 13).
var idPattern = regexp.MustCompile(`^mem:(\d{4}-\d{2}-\d{2})-([a-z0-9]+(?:-[a-z0-9]+)*)$`)

// NewID constructs an ID from a date and a free-form label, slugifying the
// label into kebab-case. It returns an error if the label slugifies to empty
// (no usable characters), since an ID with no slug is not well-formed.
func NewID(date time.Time, label string) (ID, error) {
	slug := Slugify(label)
	if slug == "" {
		return "", fmt.Errorf("label %q produced an empty slug", label)
	}
	id := ID(fmt.Sprintf("%s%s-%s", idPrefix, date.Format("2006-01-02"), slug))
	if err := id.Validate(); err != nil {
		return "", err
	}
	return id, nil
}

// Validate checks that the ID conforms to the grammar and carries a real
// calendar date.
func (id ID) Validate() error {
	if id == "" {
		return fmt.Errorf("must not be empty")
	}
	m := idPattern.FindStringSubmatch(string(id))
	if m == nil {
		return fmt.Errorf("malformed id %q: want mem:YYYY-MM-DD-<kebab-slug>", string(id))
	}
	if _, err := time.Parse("2006-01-02", m[1]); err != nil {
		return fmt.Errorf("malformed id %q: invalid date %q", string(id), m[1])
	}
	return nil
}

// Date returns the date component encoded in the ID. The ID must be valid.
func (id ID) Date() (time.Time, error) {
	m := idPattern.FindStringSubmatch(string(id))
	if m == nil {
		return time.Time{}, fmt.Errorf("malformed id %q", string(id))
	}
	return time.Parse("2006-01-02", m[1])
}

// Slug returns the slug component (everything after `mem:YYYY-MM-DD-`).
func (id ID) Slug() (string, error) {
	m := idPattern.FindStringSubmatch(string(id))
	if m == nil {
		return "", fmt.Errorf("malformed id %q", string(id))
	}
	return m[2], nil
}

func (id ID) String() string { return string(id) }

// slugStripRe removes any character that isn't a lowercase letter, digit, or
// space/hyphen, after lowercasing. Applied before collapsing whitespace.
var slugStripRe = regexp.MustCompile(`[^a-z0-9\s-]+`)

// slugSepRe collapses runs of whitespace and hyphens into a single hyphen.
var slugSepRe = regexp.MustCompile(`[\s-]+`)

// Slugify converts an arbitrary label into a kebab-case slug suitable for an
// ID: lowercase, alphanumerics separated by single hyphens, no leading or
// trailing hyphens. Returns "" if nothing usable remains.
func Slugify(label string) string {
	s := strings.ToLower(strings.TrimSpace(label))
	s = slugStripRe.ReplaceAllString(s, "")
	s = slugSepRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
}
