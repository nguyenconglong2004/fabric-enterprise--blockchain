package discovery

import "strings"

// ParseBootstraps returns unique non-empty multiaddrs from comma-separated input.
func ParseBootstraps(single, plural string) []string {
	seen := make(map[string]struct{})
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	for _, part := range strings.Split(plural, ",") {
		add(part)
	}
	add(single)
	return out
}
