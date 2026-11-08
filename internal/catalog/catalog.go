// Package catalog loads tasks and the learning plan from files on disk.
package catalog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Lang is a solution language.
type Lang string

const (
	Go     Lang = "go"
	Python Lang = "python"
	Rust   Lang = "rust"
)

// langDir maps a language to its directory inside a task folder.
var langDir = map[Lang]string{Go: "go", Python: "py", Rust: "rs"}

// LangFiles is what a task provides for one language.
type LangFiles struct {
	Stub    string // what the user starts with
	Harness string // glue that decodes args and calls the user's function
}

// Test is one test case: positional arguments and the expected result.
type Test struct {
	Args   []json.RawMessage `json:"args"`
	Want   json.RawMessage   `json:"want"`
	Public bool              `json:"public"`
}

// Task is one problem.
type Task struct {
	Slug        string `yaml:"-"`
	Title       string `yaml:"title"`
	Difficulty  string `yaml:"difficulty"` // easy | medium | hard
	Pattern     string `yaml:"pattern"`    // NeetCode topic
	Month       int    `yaml:"month"`      // plan month 1..6
	Link        string `yaml:"link"`
	Compare     string `yaml:"compare"`       // exact | unordered
	TimeLimitMS int    `yaml:"time_limit_ms"` // for the whole test run
	Order       int    `yaml:"order"`

	Statement string             `yaml:"-"`
	Tests     []Test             `yaml:"-"`
	Langs     map[Lang]LangFiles `yaml:"-"`
}

// PublicTests returns tests shown to the user and used by :run.
func (t *Task) PublicTests() []Test {
	var out []Test
	for _, tc := range t.Tests {
		if tc.Public {
			out = append(out, tc)
		}
	}
	return out
}

// LangList returns the task's languages in a stable order.
func (t *Task) LangList() []Lang {
	var out []Lang
	for _, l := range []Lang{Go, Python, Rust} {
		if _, ok := t.Langs[l]; ok {
			out = append(out, l)
		}
	}
	return out
}

// Catalog holds every task, sorted by plan month and order.
type Catalog struct {
	Tasks  []*Task
	bySlug map[string]*Task
	Plan   *Plan
}

// Get returns a task by slug.
func (c *Catalog) Get(slug string) (*Task, bool) {
	t, ok := c.bySlug[slug]
	return t, ok
}

// Load reads every task folder under dir/tasks and dir/plan.yaml.
func Load(dir string) (*Catalog, error) {
	c := &Catalog{bySlug: map[string]*Task{}}
	entries, err := os.ReadDir(filepath.Join(dir, "tasks"))
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		t, err := loadTask(filepath.Join(dir, "tasks", e.Name()))
		if err != nil {
			return nil, fmt.Errorf("task %s: %w", e.Name(), err)
		}
		c.Tasks = append(c.Tasks, t)
		c.bySlug[t.Slug] = t
	}
	sort.SliceStable(c.Tasks, func(i, j int) bool {
		a, b := c.Tasks[i], c.Tasks[j]
		if a.Month != b.Month {
			return a.Month < b.Month
		}
		if a.Order != b.Order {
			return a.Order < b.Order
		}
		return a.Slug < b.Slug
	})
	plan, err := LoadPlan(filepath.Join(dir, "plan.yaml"))
	if err != nil {
		return nil, err
	}
	c.Plan = plan
	return c, nil
}

func loadTask(dir string) (*Task, error) {
	t := &Task{Slug: filepath.Base(dir), Compare: "exact", TimeLimitMS: 2000}
	raw, err := os.ReadFile(filepath.Join(dir, "task.yaml"))
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(raw, t); err != nil {
		return nil, err
	}
	if t.Compare != "exact" && t.Compare != "unordered" {
		return nil, fmt.Errorf("unknown compare %q", t.Compare)
	}
	st, err := os.ReadFile(filepath.Join(dir, "statement.md"))
	if err != nil {
		return nil, err
	}
	t.Statement = strings.TrimSpace(string(st))
	tests, err := os.ReadFile(filepath.Join(dir, "tests.json"))
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(tests, &t.Tests); err != nil {
		return nil, fmt.Errorf("tests.json: %w", err)
	}
	if len(t.Tests) == 0 {
		return nil, fmt.Errorf("no tests")
	}
	t.Langs = map[Lang]LangFiles{}
	for lang, sub := range langDir {
		ext := map[Lang]string{Go: ".go", Python: ".py", Rust: ".rs"}[lang]
		stub, err1 := os.ReadFile(filepath.Join(dir, sub, "stub"+ext))
		harness, err2 := os.ReadFile(filepath.Join(dir, sub, "harness"+ext))
		if err1 != nil || err2 != nil {
			continue
		}
		t.Langs[lang] = LangFiles{Stub: string(stub), Harness: string(harness)}
	}
	if len(t.Langs) == 0 {
		return nil, fmt.Errorf("no languages")
	}
	return t, nil
}
