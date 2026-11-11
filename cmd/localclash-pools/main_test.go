package main

import (
	"testing"
	"time"
)

func TestNextRun(t *testing.T) {
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	// The next day begins daylight saving time, 23 hours after today's 08:00.
	now := time.Date(2026, time.March, 7, 8, 0, 0, 0, location)
	for period, elapsed := range map[string]time.Duration{
		"5m": 5 * time.Minute, "15m": 15 * time.Minute, "30m": 30 * time.Minute,
		"1h": time.Hour, "daily-08": 23 * time.Hour,
	} {
		got, err := nextRun(now, period)
		if err != nil || !got.Equal(now.Add(elapsed)) {
			t.Fatalf("nextRun(%q) = %v, %v; want %v", period, got, err, now.Add(elapsed))
		}
	}
	before := now.Add(-time.Second)
	if got, err := nextRun(before, "daily-08"); err != nil || !got.Equal(now) {
		t.Fatalf("next daily run before 08:00 = %v, %v; want %v", got, err, now)
	}
	if _, err := nextRun(now, "10m"); err == nil {
		t.Fatal("unknown period was accepted")
	}
}
