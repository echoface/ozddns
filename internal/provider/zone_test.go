package provider

import (
	"testing"

	"github.com/echoface/ozddns/internal/domain"
)

func TestSuffixes(t *testing.T) {
	got := Suffixes("home.example.com.")
	want := []string{"home.example.com", "example.com", "com"}
	if len(got) != len(want) {
		t.Fatalf("Suffixes = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Suffixes = %v, want %v", got, want)
		}
	}
}

// REC-08: wildcard and apex names resolve to the right zone + relative name.
func TestLongestZoneMatch(t *testing.T) {
	zones := []domain.Zone{{Name: "example.com"}, {Name: "com"}}
	z, rel, ok := LongestZoneMatch(zones, "home.example.com")
	if !ok || z.Name != "example.com" || rel != "home" {
		t.Fatalf("got %v %q %v", z, rel, ok)
	}
	_, rel, _ = LongestZoneMatch(zones, "example.com")
	if rel != "@" {
		t.Fatalf("apex relative = %q", rel)
	}
	_, rel, _ = LongestZoneMatch([]domain.Zone{{Name: "ozhao.cn"}}, "*.ozhao.cn")
	if rel != "*" {
		t.Fatalf("wildcard relative = %q", rel)
	}
	if _, _, ok := LongestZoneMatch([]domain.Zone{{Name: "other.org"}}, "home.example.com"); ok {
		t.Fatal("expected no zone match")
	}
}

func TestIsSubdomain(t *testing.T) {
	cases := []struct {
		name, zone string
		want       bool
	}{
		{"home.example.com", "example.com", true},
		{"example.com", "example.com", true},
		{"notexample.com", "example.com", false},
		{"HOME.EXAMPLE.COM", "example.com", true},
	}
	for _, c := range cases {
		if got := IsSubdomain(c.name, c.zone); got != c.want {
			t.Errorf("IsSubdomain(%q,%q)=%v want %v", c.name, c.zone, got, c.want)
		}
	}
}
