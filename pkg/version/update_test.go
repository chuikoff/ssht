package version

import "testing"

func TestIsNewer(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"v1.3.11", "1.3.10", true},
		{"1.3.10", "v1.3.10", false},
		{"1.3.9", "1.3.10", false},
		{"1.4.0", "1.3.10", true},
		{"2", "1.9.9", true},
		{"", "1.3.10", false},
		{"v1.3.11-rc", "1.3.10", true},
	}
	for _, c := range cases {
		if got := IsNewer(c.latest, c.current); got != c.want {
			t.Errorf("IsNewer(%q, %q)=%v, want %v", c.latest, c.current, got, c.want)
		}
	}
}
