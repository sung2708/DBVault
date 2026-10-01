// Package doctor defines the stable, secret-free readiness report.
package doctor

import "fmt"

type Status string

const (
	Pass Status = "pass"
	Warn Status = "warning"
	Fail Status = "fail"
	Skip Status = "skipped"
)

type Check struct {
	Name        string `json:"name"`
	Category    string `json:"category"`
	Status      Status `json:"status"`
	Summary     string `json:"summary"`
	Detail      string `json:"detail,omitempty"`
	Remediation string `json:"remediation,omitempty"`
}

type Counts struct {
	Passed   int `json:"passed"`
	Warnings int `json:"warnings"`
	Failed   int `json:"failed"`
	Skipped  int `json:"skipped"`
}

type Report struct {
	Ready   bool    `json:"ready"`
	Summary Counts  `json:"summary"`
	Checks  []Check `json:"checks"`
}

func (r *Report) Add(check Check) { r.Checks = append(r.Checks, check) }

func (r *Report) Finalize() {
	r.Summary = Counts{}
	r.Ready = true
	for _, c := range r.Checks {
		switch c.Status {
		case Pass:
			r.Summary.Passed++
		case Warn:
			r.Summary.Warnings++
		case Fail:
			r.Summary.Failed++
			r.Ready = false
		case Skip:
			r.Summary.Skipped++
		}
	}
}

type ReadinessError struct{ Report Report }

func (e *ReadinessError) Error() string {
	return fmt.Sprintf("readiness checks failed (%d failed)", e.Report.Summary.Failed)
}
