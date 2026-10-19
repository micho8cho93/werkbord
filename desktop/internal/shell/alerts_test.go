package shell

import (
	"strings"
	"testing"
	"time"
)

type fakeAlerter struct {
	status string
	shown  []shownAlert
	opened int
}

type shownAlert struct {
	title, body    string
	urgent, forced bool
}

func (f *fakeAlerter) Status() string { return f.status }
func (f *fakeAlerter) Show(title, body string, urgent, force bool) bool {
	f.shown = append(f.shown, shownAlert{title, body, urgent, force})
	return true
}
func (f *fakeAlerter) OpenSettings() { f.opened++ }

func TestAlertsAreShortPlainAndSlow(t *testing.T) {
	f := &fakeAlerter{status: "granted"}
	s := New(Options{Alerter: f})
	if got := s.AlertStatus(); got != "granted" {
		t.Fatalf("status %q", got)
	}
	if !s.Alert("Needs\x07  you", "Shop  ·\n Fix login", true) {
		t.Fatal("an ordinary alert was refused")
	}
	if got := f.shown[0]; got != (shownAlert{"Needs you", "Shop · Fix login", true, false}) {
		t.Fatalf("shown %+v", got)
	}
	if s.Alert("   ", "empty title", false) {
		t.Error("an alert with no title was shown")
	}
	s.Alert(strings.Repeat("x", 500), strings.Repeat("é", 500), false)
	if got := f.shown[1]; len([]rune(got.title)) != 80 || len([]rune(got.body)) != 200 || !strings.HasSuffix(got.body, "…") {
		t.Errorf("long text was not cut: %d %d", len([]rune(got.title)), len([]rune(got.body)))
	}
	shown := len(f.shown)
	for i := 0; i < 50; i++ {
		s.Alert("again", "and again", false)
	}
	if len(f.shown)-shown > alertMax {
		t.Errorf("%d alerts in a minute, want at most %d", len(f.shown), alertMax)
	}
}

func TestTheAlertRateLimitForgetsAfterAMinute(t *testing.T) {
	s := New(Options{Alerter: &fakeAlerter{}})
	now := time.Now()
	for i := 0; i < alertMax; i++ {
		if !s.alertAllowed(now) {
			t.Fatalf("alert %d refused", i)
		}
	}
	if s.alertAllowed(now) {
		t.Error("over the limit")
	}
	if !s.alertAllowed(now.Add(alertWindow + time.Second)) {
		t.Error("the limit did not reset")
	}
}

func TestAWindowWithoutSystemAlertsSaysSoAndShowsNothing(t *testing.T) {
	s := New(Options{})
	if s.AlertStatus() != "unsupported" || s.Alert("a", "b", false) || s.TestAlert() {
		t.Error("alerts were offered with no alerter")
	}
	s.OpenAlertSettings()
	f := &fakeAlerter{}
	s = New(Options{Alerter: f})
	if !s.TestAlert() || !f.shown[0].forced {
		t.Errorf("the test alert is shown even with the app in front: %+v", f.shown)
	}
	s.OpenAlertSettings()
	if f.opened != 1 {
		t.Error("notification settings were not opened")
	}
}
