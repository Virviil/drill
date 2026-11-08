// Package judge compiles and runs solutions against a task's tests.
package judge

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Virviil/drill/internal/catalog"
)

//go:embed golang/common.go.tmpl
var goCommon string

// JobsDir is where job directories are created ("" = system temp).
// When drill itself runs in a container and talks to the host's Docker,
// this must be a path that is identical on the host and in the container.
var JobsDir = ""

// Status of a finished job.
type Status string

const (
	StatusOK           Status = "ok"
	StatusWrong        Status = "wrong"
	StatusCompileError Status = "compile_error"
	StatusRuntimeError Status = "runtime_error"
	StatusTLE          Status = "tle"
	StatusMLE          Status = "mle"
	StatusInternal     Status = "internal_error"
)

// Human-readable status in Russian.
func (s Status) String() string {
	switch s {
	case StatusOK:
		return "Принято"
	case StatusWrong:
		return "Неверный ответ"
	case StatusCompileError:
		return "Ошибка компиляции"
	case StatusRuntimeError:
		return "Ошибка выполнения"
	case StatusTLE:
		return "Превышен лимит времени"
	case StatusMLE:
		return "Превышен лимит памяти"
	default:
		return "Внутренняя ошибка судьи"
	}
}

// Job is one request to check code.
type Job struct {
	UserID int64
	Task   *catalog.Task
	Lang   catalog.Lang
	Code   string
	Submit bool // false = only public tests
}

// Failure describes the first failed test.
type Failure struct {
	Index  int    // 1-based among the tests that were run
	Hidden bool   // input not shown
	Input  string // JSON args, may be truncated
	Want   string
	Got    string
	Err    string
}

// Result of a job.
type Result struct {
	Status  Status
	Passed  int
	Total   int
	TimeMS  int
	Failure *Failure
	Output  string // compiler output or stderr of the solution, truncated
}

// Sandbox compiles and runs one prepared directory.
type Sandbox interface {
	// Run builds the program in dir and feeds it input.json.
	// It returns stdout, stderr and the exit code (see exit codes below).
	Run(ctx context.Context, lang catalog.Lang, dir string, timeLimitMS int) (stdout, stderr []byte, code int, err error)
}

// Exit codes the sandbox scripts use.
const (
	exitCompile = 101
	exitTimeout = 124
	exitKilled  = 137 // SIGKILL: usually the OOM killer
	exitTERM    = 143
)

const maxOutput = 8 << 10

// Check prepares files, runs the sandbox and grades the result.
func Check(ctx context.Context, sb Sandbox, job Job) Result {
	tests := job.Task.Tests
	if !job.Submit {
		tests = job.Task.PublicTests()
	}
	res := Result{Total: len(tests)}

	dir, err := os.MkdirTemp(JobsDir, "drill-job-")
	if err != nil {
		return internal(res, err)
	}
	defer os.RemoveAll(dir)
	if err := os.Chmod(dir, 0o755); err != nil {
		return internal(res, err)
	}
	if err := writeFiles(dir, job, tests); err != nil {
		return internal(res, err)
	}

	stdout, stderr, code, err := sb.Run(ctx, job.Lang, dir, job.Task.TimeLimitMS)
	if err != nil {
		return internal(res, err)
	}
	res.Output = truncate(string(stderr), maxOutput)
	switch code {
	case 0:
	case exitCompile:
		res.Status = StatusCompileError
		return res
	case exitTimeout, exitTERM:
		res.Status = StatusTLE
	case exitKilled:
		res.Status = StatusMLE
	default:
		res.Status = StatusRuntimeError
	}

	lines, timeMS := parseOutput(stdout)
	res.TimeMS = timeMS
	for i, tc := range tests {
		if i >= len(lines) {
			if res.Status == "" {
				res.Status = StatusRuntimeError
			}
			res.Failure = failure(i, tc, "", "нет ответа")
			return res
		}
		line := lines[i]
		if line.Err != "" {
			res.Status = StatusRuntimeError
			res.Failure = failure(i, tc, "", line.Err)
			return res
		}
		if !Equal(tc.Want, line.Got, job.Task.Compare) {
			res.Status = StatusWrong
			res.Failure = failure(i, tc, string(line.Got), "")
			return res
		}
		res.Passed++
	}
	if res.Status == "" {
		res.Status = StatusOK
	}
	return res
}

func internal(res Result, err error) Result {
	res.Status = StatusInternal
	res.Output = err.Error()
	return res
}

func failure(i int, tc catalog.Test, got, errText string) *Failure {
	args, _ := json.Marshal(tc.Args)
	return &Failure{
		Index:  i + 1,
		Hidden: !tc.Public,
		Input:  truncate(string(args), 300),
		Want:   truncate(string(tc.Want), 200),
		Got:    truncate(got, 200),
		Err:    truncate(errText, 500),
	}
}

type outLine struct {
	Got json.RawMessage `json:"got"`
	Err string          `json:"err"`
	// final line
	TimeMS *int `json:"time_ms"`
}

func parseOutput(stdout []byte) ([]outLine, int) {
	var lines []outLine
	timeMS := 0
	sc := bufio.NewScanner(bytes.NewReader(stdout))
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		var l outLine
		if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
			continue
		}
		if l.TimeMS != nil {
			timeMS = *l.TimeMS
			continue
		}
		lines = append(lines, l)
	}
	return lines, timeMS
}

func writeFiles(dir string, job Job, tests []catalog.Test) error {
	lf, ok := job.Task.Langs[job.Lang]
	if !ok {
		return fmt.Errorf("task %s has no %s version", job.Task.Slug, job.Lang)
	}
	args := make([][]json.RawMessage, len(tests))
	for i, tc := range tests {
		args[i] = tc.Args
	}
	input, err := json.Marshal(args)
	if err != nil {
		return err
	}
	files := map[string]string{"input.json": string(input)}
	switch job.Lang {
	case catalog.Go:
		files["go.mod"] = "module solution\n\ngo 1.25\n"
		files["solution.go"] = job.Code
		files["harness.go"] = lf.Harness
		files["common.go"] = goCommon
	default:
		return fmt.Errorf("язык %s пока не поддерживается судьёй", job.Lang)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// Equal compares two JSON values. null and [] are equal (Go nil slices).
// With mode "unordered" the top-level arrays are compared as multisets.
func Equal(want, got json.RawMessage, mode string) bool {
	var w, g any
	if json.Unmarshal(want, &w) != nil {
		return false
	}
	if len(bytes.TrimSpace(got)) == 0 {
		got = json.RawMessage("null")
	}
	if json.Unmarshal(got, &g) != nil {
		return false
	}
	w, g = normNull(w), normNull(g)
	if mode == "unordered" {
		wa, ok1 := w.([]any)
		ga, ok2 := g.([]any)
		if ok1 && ok2 {
			return canonSorted(wa) == canonSorted(ga)
		}
	}
	return canon(w) == canon(g)
}

func normNull(v any) any {
	if v == nil {
		return []any{}
	}
	return v
}

func canon(v any) string {
	b, _ := json.Marshal(v) // map keys are sorted by encoding/json
	return string(b)
}

func canonSorted(a []any) string {
	parts := make([]string, len(a))
	for i, v := range a {
		parts[i] = canon(v)
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
