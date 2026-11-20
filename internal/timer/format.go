// SPDX-License-Identifier: AGPL-3.0-only

// Package timer provides duration formatting helpers.
package timer

import (
	"fmt"
	"time"
)

// Duration formats seconds as HH:MM:SS.
func Duration(seconds int64) string {
	if seconds < 0 {
		return "-" + Duration(-seconds)
	}
	return fmt.Sprintf("%02d:%02d:%02d", seconds/3600, (seconds%3600)/60, seconds%60)
}

// SignedDuration formats seconds as +HH:MM:SS or -HH:MM:SS.
func SignedDuration(seconds int64) string {
	if seconds < 0 {
		return "-" + Duration(-seconds)
	}
	return "+" + Duration(seconds)
}

// Minutes formats a minute count in a human friendly way.
func Minutes(minutes int64) string {
	if minutes < 60 {
		return fmt.Sprintf("%d min", minutes)
	}
	return fmt.Sprintf("%d h %d min", minutes/60, minutes%60)
}

// DateTime formats a Unix timestamp in the local timezone.
func DateTime(ts int64) string {
	if ts == 0 {
		return "—"
	}
	return time.Unix(ts, 0).Local().Format("02.01.2006 15:04")
}
