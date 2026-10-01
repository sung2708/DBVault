package doctor

import "testing"

func TestFinalizeReadinessAndCounts(t *testing.T) {
	r := Report{}
	for _, status := range []Status{Pass, Pass, Warn, Skip} {
		r.Add(Check{Status: status})
	}
	r.Finalize()
	if !r.Ready || r.Summary != (Counts{Passed: 2, Warnings: 1, Skipped: 1}) {
		t.Fatalf("unexpected warning-only report: %+v", r)
	}
	r.Add(Check{Status: Fail})
	r.Finalize()
	if r.Ready || r.Summary.Failed != 1 {
		t.Fatalf("failure did not block readiness: %+v", r)
	}
}
