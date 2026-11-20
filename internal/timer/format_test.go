// SPDX-License-Identifier: AGPL-3.0-only

package timer

import "testing"

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
