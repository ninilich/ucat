package config

import "testing"

func TestJoinKey(t *testing.T) {
	if got := joinKey(); got != "" {
		t.Errorf("no parts: %q", got)
	}
	if got := joinKey("a"); got != "a" {
		t.Errorf("one part: %q", got)
	}
	if got := joinKey("a", "b", "c"); got != "a\x00b\x00c" {
		t.Errorf("three parts: %q", got)
	}
	// Part boundaries matter: the same characters split differently are different keys.
	pairs := [][2][]string{
		{{"a", "bc"}, {"ab", "c"}},
		{{"", "a"}, {"a", ""}},
		{{"a", ""}, {"a"}},
		{{"", ""}, {""}},
		{{"ü", "x"}, {"üx", ""}},
	}
	for _, p := range pairs {
		if joinKey(p[0]...) == joinKey(p[1]...) {
			t.Errorf("collision: %q and %q", p[0], p[1])
		}
	}
}

func TestJoinAnd(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{nil, ""},
		{[]string{}, ""},
		{[]string{"a"}, "a"},
		{[]string{"a", "b"}, "a and b"},
		{[]string{"a", "b", "c"}, "a, b and c"},
		{[]string{"a", "b", "c", "d"}, "a, b, c and d"},
		{[]string{"", ""}, " and "},
		{[]string{`"x" (f.yml)`, "ü and"}, `"x" (f.yml) and ü and`},
	}
	for _, c := range cases {
		if got := joinAnd(c.in); got != c.want {
			t.Errorf("joinAnd(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestBoth(t *testing.T) {
	for n, want := range map[int]string{0: litBoth, 1: "both", 2: "both", 3: "all", 10: "all"} {
		if got := both(n, "all"); got != want {
			t.Errorf("both(%d) = %q, want %q", n, got, want)
		}
	}
	if got := both(3, "all of"); got != "all of" {
		t.Errorf("both(3, all of) = %q", got)
	}
}
