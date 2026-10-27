package version

import "testing"

func TestKind(t *testing.T) {
	for v, want := range map[string]string{
		"v0.1.0":                               KindRelease,
		"v1.2.3-rc.1":                          KindRelease,
		"dev":                                  KindDev,
		"(devel)":                              KindDev,
		"1.2.3":                                KindDev,
		"v0.0.0-20261016123456-abcdef123456":   KindPseudo,
		"v0.1.1-0.20261016123456-abcdef123456": KindPseudo,
		"v0.2.0-rc.1.0.20261016123456-abcdef123456":       KindPseudo,
		"v0.0.0-20261016123456-abcdef123456+dirty":        KindPseudo,
		"v0.0.0-20261016123456-abcdef123456+incompatible": KindPseudo,
	} {
		if got := Kind(v); got != want {
			t.Errorf("Kind(%q) = %s, want %s", v, got, want)
		}
	}
}

func TestNewer(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"v0.2.0", "v0.1.0", true},
		{"v0.1.0", "v0.2.0", false},
		{"v0.1.0", "v0.1.0", false},
		{"v1.0.0", "v0.9.9", true},
		{"v0.10.0", "v0.9.0", true},
		{"v0.1.0", "v0.1.0-rc.1", true},
		{"v0.1.0-rc.1", "v0.1.0", false},
		{"v0.1.0-rc.2", "v0.1.0-rc.1", true},
		{"v0.1.0-rc.10", "v0.1.0-rc.9", true},
		{"v0.1.0-beta", "v0.1.0-alpha", true},
		{"v0.1.0-rc.1.1", "v0.1.0-rc.1", true},
		{"latest", "v0.1.0", false},
		{"v0.1.0", "dev", false},
	} {
		if got := Newer(tc.a, tc.b); got != tc.want {
			t.Errorf("Newer(%s, %s) = %v", tc.a, tc.b, got)
		}
	}
}
