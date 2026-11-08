package catalog

import (
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Plan is the six-month learning plan.
type Plan struct {
	Start  string  `yaml:"start"` // YYYY-MM, first month
	Months []Month `yaml:"months"`
}

// Month is one month of the plan.
type Month struct {
	N         int        `yaml:"n"`
	Title     string     `yaml:"title"`
	Lang      Lang       `yaml:"lang"` // language for LeetCode this month
	Topics    string     `yaml:"topics"`
	Items     []PlanItem `yaml:"items"`
	Milestone string     `yaml:"milestone"`
}

// PlanItem is something to tick off: a chapter, a project step, a reading.
type PlanItem struct {
	ID    string `yaml:"id"`
	Kind  string `yaml:"kind"` // theory | project | reading | checkpoint
	Title string `yaml:"title"`
}

// LoadPlan reads plan.yaml.
func LoadPlan(path string) (*Plan, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	p := &Plan{}
	if err := yaml.Unmarshal(raw, p); err != nil {
		return nil, err
	}
	return p, nil
}

// Current returns the plan month for the given moment (clamped to 1..len).
func (p *Plan) Current(now time.Time) *Month {
	if len(p.Months) == 0 {
		return nil
	}
	start, err := time.Parse("2006-01", p.Start)
	if err != nil {
		return &p.Months[0]
	}
	n := (now.Year()-start.Year())*12 + int(now.Month()-start.Month())
	if n < 0 {
		n = 0
	}
	if n >= len(p.Months) {
		n = len(p.Months) - 1
	}
	return &p.Months[n]
}
