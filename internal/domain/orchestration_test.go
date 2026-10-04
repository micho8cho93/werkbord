package domain

import (
	"testing"
	"time"
)

func TestOrchestrationValidation(t *testing.T) {
	o := Orchestration{Timezone: "Europe/Madrid", MissedPolicy: "skip", GraceSeconds: 300}
	o.Normalize()
	if e := o.Validate(); e != nil {
		t.Fatal(e)
	}
	at, _ := time.Parse(time.RFC3339, "2026-10-04T01:00:00+02:00")
	o.ScheduledAt = &at
	o.Normalize()
	if o.ScheduledAt.Format(time.RFC3339) != "2026-10-03T23:00:00Z" {
		t.Fatal(o.ScheduledAt)
	}
	for _, bad := range []Orchestration{{Timezone: "no/such", MissedPolicy: "skip"}, {Timezone: "UTC", MissedPolicy: "guess"}, {Timezone: "UTC", MissedPolicy: "skip", ExpectedPaths: []string{"../secret"}}, {Timezone: "UTC", MissedPolicy: "skip", TargetCommit: "main"}, {Timezone: "UTC", MissedPolicy: "skip", GraceSeconds: -1}} {
		if bad.Validate() == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
}
func TestDependencyCycle(t *testing.T) {
	cases := []struct {
		name  string
		tasks []Task
		cycle bool
	}{
		{"chain", []Task{{ID: "a"}, {ID: "b", Orchestration: Orchestration{Dependencies: []string{"a"}}}, {ID: "c", Orchestration: Orchestration{Dependencies: []string{"b", "a"}}}}, false},
		{"self", []Task{{ID: "a", Orchestration: Orchestration{Dependencies: []string{"a"}}}}, true},
		{"indirect", []Task{{ID: "a", Orchestration: Orchestration{Dependencies: []string{"c"}}}, {ID: "b", Orchestration: Orchestration{Dependencies: []string{"a"}}}, {ID: "c", Orchestration: Orchestration{Dependencies: []string{"b"}}}}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := DependencyCycle(c.tasks); got != c.cycle {
				t.Fatal(got)
			}
		})
	}
}
