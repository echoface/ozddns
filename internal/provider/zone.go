package provider

import (
	"strings"

	"github.com/echoface/ozddns/internal/domain"
)

// Suffixes returns candidate zone names for a record name, longest first:
// "home.example.com" -> ["home.example.com", "example.com", "com"].
func Suffixes(name string) []string {
	name = strings.TrimSuffix(strings.TrimSpace(name), ".")
	if name == "" {
		return nil
	}
	parts := strings.Split(name, ".")
	out := make([]string, 0, len(parts))
	for i := 0; i < len(parts); i++ {
		out = append(out, strings.Join(parts[i:], "."))
	}
	return out
}

// LongestZoneMatch finds the zone that is the longest suffix of name and returns
// it together with the relative record name ("@" when the name equals the zone).
func LongestZoneMatch(zones []domain.Zone, name string) (domain.Zone, string, bool) {
	name = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(name)), ".")
	best := domain.Zone{}
	found := false
	for _, z := range zones {
		if !IsSubdomain(name, z.Name) {
			continue
		}
		if !found || len(z.Name) > len(best.Name) {
			best = z
			found = true
		}
	}
	if !found {
		return domain.Zone{}, "", false
	}
	return best, RelativeName(best.Name, name), true
}

// IsSubdomain reports whether name belongs to zone (case-insensitive).
func IsSubdomain(name, zone string) bool {
	name = strings.ToLower(strings.TrimSuffix(name, "."))
	zone = strings.ToLower(strings.TrimSuffix(zone, "."))
	if name == zone {
		return true
	}
	return strings.HasSuffix(name, "."+zone)
}

// RelativeName returns the record name relative to a zone ("@" for apex).
func RelativeName(zone, name string) string {
	zone = strings.ToLower(strings.TrimSuffix(zone, "."))
	name = strings.ToLower(strings.TrimSuffix(name, "."))
	if name == zone {
		return "@"
	}
	return strings.TrimSuffix(name, "."+zone)
}
