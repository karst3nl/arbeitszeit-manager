// SPDX-License-Identifier: AGPL-3.0-only

package timer

import (
	"testing"
)

func TestDuration(t *testing.T) {
	cases := map[int64]string{
		0:    "00:00:00",
		59:   "00:00:59",
		60:   "00:01:00",
		3661: "01:01:01",
	}
	for in, want := range cases {
		if got := Duration(in); got != want {
			t.Errorf("Duration(%d) = %q, want %q", in, got, want)
		}
	}
	if got := Duration(-90); got != "-00:01:30" {
		t.Errorf("Duration(-90) = %q", got)
	}
}

func TestMinutes(t *testing.T) {
	if got := Minutes(45); got != "45 min" {
		t.Errorf("Minutes(45) = %q", got)
	}
	if got := Minutes(150); got != "2 h 30 min" {
		t.Errorf("Minutes(150) = %q", got)
	}
}

func TestAWFormatting(t *testing.T) {
	for _, tc := range []struct {
		minutes int64
		want    string
	}{
		{60, "10 AW"},
		{180, "30 AW"},
		{15, "2,5 AW"},
		{7, "1,2 AW"},
		{-15, "-2,5 AW"},
	} {
		if got := AWFromMinutes(tc.minutes); got != tc.want {
			t.Errorf("AWFromMinutes(%d) = %q, want %q", tc.minutes, got, tc.want)
		}
	}
	if got := AWFromSeconds(5400); got != "15 AW" {
		t.Errorf("AWFromSeconds(5400) = %q", got)
	}
}

func TestParseAW(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  int64
	}{
		{"10", 60},
		{"2,5", 15},
		{"0,5", 3},
		{" 20 ", 120},
	} {
		got, err := ParseAW(tc.input)
		if err != nil || got != tc.want {
			t.Errorf("ParseAW(%q) = %d, %v; want %d", tc.input, got, err, tc.want)
		}
	}
	for _, input := range []string{"", "0", "-1", "abc"} {
		if _, err := ParseAW(input); err == nil {
			t.Errorf("ParseAW(%q) accepted invalid value", input)
		}
	}
}
