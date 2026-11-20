// SPDX-License-Identifier: AGPL-3.0-only

// Package timer provides duration formatting helpers.
package timer

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// DefaultMinutesPerAW is the number of minutes one Arbeitswert (AW)
// represents until an admin changes the setting. Ten AW equal one hour.
const DefaultMinutesPerAW = 6

// MaxPlannedMinutes is the upper bound for the Soll entered for an order.
const MaxPlannedMinutes = 60000

// NormalizeMinutesPerAW guards against an invalid or unset setting and falls
// back to the default conversion.
func NormalizeMinutesPerAW(factor float64) float64 {
	if math.IsNaN(factor) || math.IsInf(factor, 0) || factor <= 0 {
		return DefaultMinutesPerAW
	}
	return factor
}

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

// AWFromMinutes formats a minute count as Arbeitswerte with at most one
// decimal place and a German comma as separator.
func AWFromMinutes(minutes int64, minutesPerAW float64) string {
	return formatAW(float64(minutes) / NormalizeMinutesPerAW(minutesPerAW))
}

// AWFromSeconds formats a second count as Arbeitswerte.
func AWFromSeconds(seconds int64, minutesPerAW float64) string {
	return formatAW(float64(seconds) / 60 / NormalizeMinutesPerAW(minutesPerAW))
}

func formatAW(value float64) string {
	text := strconv.FormatFloat(math.Round(value*10)/10, 'f', 1, 64)
	text = strings.ReplaceAll(text, ".", ",")
	text = strings.TrimSuffix(text, ",0")
	return text + " AW"
}

// ParseAW converts an AW input (decimal comma or point allowed) into whole
// minutes using the configured number of minutes per AW.
func ParseAW(value string, minutesPerAW float64) (int64, error) {
	factor := NormalizeMinutesPerAW(minutesPerAW)
	aw, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(value), ",", "."), 64)
	if err != nil || math.IsNaN(aw) || math.IsInf(aw, 0) || aw <= 0 || aw > MaxPlannedMinutes/factor {
		return 0, fmt.Errorf("ungültiger AW-Wert")
	}
	minutes := int64(math.Round(aw * factor))
	if minutes < 1 {
		minutes = 1
	}
	return minutes, nil
}

// DateTime formats a Unix timestamp in the local timezone.
func DateTime(ts int64) string {
	if ts == 0 {
		return "—"
	}
	return time.Unix(ts, 0).Local().Format("02.01.2006 15:04")
}
