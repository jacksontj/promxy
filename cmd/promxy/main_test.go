package main

import (
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/rules"

	"github.com/jacksontj/promxy/pkg/alerttemplate"
)

func TestNotifierAlerts(t *testing.T) {
	fired := time.Date(2026, 10, 30, 16, 0, 0, 0, time.UTC)
	validUntil := fired.Add(4 * time.Hour)
	resolved := fired.Add(2 * time.Hour)

	firing := &rules.Alert{State: rules.StateFiring, Labels: labels.FromStrings("alertname", "Firing"), FiredAt: fired, ValidUntil: validUntil}
	done := &rules.Alert{State: rules.StateInactive, Labels: labels.FromStrings("alertname", "Done"), FiredAt: fired, ResolvedAt: resolved, ValidUntil: validUntil}
	pending := &rules.Alert{State: rules.StatePending, Labels: labels.FromStrings("alertname", "Pending"), ValidUntil: validUntil}

	got := notifierAlerts([]*rules.Alert{firing, done, pending}, "up == 0", "http://promxy", alerttemplate.NewManager())

	if len(got) != 2 {
		t.Fatalf("want 2 alerts (pending skipped), got %d", len(got))
	}
	if !got[0].EndsAt.Equal(validUntil) {
		t.Errorf("firing alert EndsAt = %v, want ValidUntil %v", got[0].EndsAt, validUntil)
	}
	if !got[1].EndsAt.Equal(resolved) {
		t.Errorf("resolved alert EndsAt = %v, want ResolvedAt %v", got[1].EndsAt, resolved)
	}
	if !got[0].StartsAt.Equal(fired) {
		t.Errorf("StartsAt = %v, want FiredAt %v", got[0].StartsAt, fired)
	}
}
