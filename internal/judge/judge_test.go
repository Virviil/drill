package judge

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Virviil/drill/internal/catalog"
)

func TestEqual(t *testing.T) {
	cases := []struct {
		want, got, mode string
		eq              bool
	}{
		{`[0,1]`, `[0,1]`, "exact", true},
		{`[0,1]`, `[1,0]`, "exact", false},
		{`[0,1]`, `[1,0]`, "unordered", true},
		{`[]`, `null`, "exact", true},
		{`true`, `false`, "exact", false},
		{`3`, `3.0`, "exact", true},
		{`[1,1,2]`, `[1,2,2]`, "unordered", false},
	}
	for _, c := range cases {
		if got := Equal(json.RawMessage(c.want), json.RawMessage(c.got), c.mode); got != c.eq {
			t.Errorf("Equal(%s,%s,%s)=%v", c.want, c.got, c.mode, got)
		}
	}
}

func sandbox(t *testing.T) Sandbox {
	if os.Getenv("DRILL_TEST_DOCKER") != "" {
		return Docker{Binary: "docker", MemoryMB: 256, CPUs: "1"}
	}
	return Local{}
}

func TestCheckTwoSum(t *testing.T) {
	cat, err := catalog.Load("../..")
	if err != nil {
		t.Fatal(err)
	}
	task, _ := cat.Get("two-sum")
	sb := sandbox(t)
	ctx := context.Background()

	good := `package main

func twoSum(nums []int, target int) []int {
	seen := map[int]int{}
	for i, v := range nums {
		if j, ok := seen[target-v]; ok {
			return []int{j, i}
		}
		seen[v] = i
	}
	return nil
}
`
	slow := `package main

import "fmt"

func twoSum(nums []int, target int) []int {
	fmt.Println("debug")
	for i := range nums {
		for j := i + 1; j < len(nums); j++ {
			if nums[i]+nums[j] == target {
				return []int{i, j}
			}
		}
	}
	return nil
}
`
	tests := []struct {
		name   string
		code   string
		submit bool
		want   Status
	}{
		{"good-run", good, false, StatusOK},
		{"good-submit", good, true, StatusOK},
		{"slow-run", slow, false, StatusOK},
		{"slow-submit", slow, true, StatusTLE},
		{"stub", task.Langs[catalog.Go].Stub, false, StatusWrong},
		{"compile", "package main\n\nfunc twoSum(nums []int, target int) []int { return x }\n", false, StatusCompileError},
		{"panic", "package main\n\nfunc twoSum(nums []int, target int) []int { var a []int; return a[:5] }\n", false, StatusRuntimeError},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := Check(ctx, sb, Job{UserID: 1, Task: task, Lang: catalog.Go, Code: tc.code, Submit: tc.submit})
			if res.Status != tc.want {
				t.Fatalf("status %s, want %s; passed %d/%d; failure %+v; output %q", res.Status, tc.want, res.Passed, res.Total, res.Failure, res.Output)
			}
			if tc.name == "slow-run" && !strings.Contains(res.Output, "debug") {
				t.Errorf("solution stdout should be captured as output, got %q", res.Output)
			}
			if tc.name == "compile" && !strings.Contains(res.Output, "undefined: x") {
				t.Errorf("compile output: %q", res.Output)
			}
			t.Logf("%s: %s %d/%d %dms", tc.name, res.Status, res.Passed, res.Total, res.TimeMS)
		})
	}
}

func TestDockerIsolation(t *testing.T) {
	if os.Getenv("DRILL_TEST_DOCKER") == "" {
		t.Skip("set DRILL_TEST_DOCKER=1")
	}
	cat, err := catalog.Load("../..")
	if err != nil {
		t.Fatal(err)
	}
	task, _ := cat.Get("two-sum")
	sb := sandbox(t)
	cases := map[string]struct {
		code string
		want Status
	}{
		"memory": {`package main

func twoSum(nums []int, target int) []int {
	b := make([]byte, 1<<30)
	for i := range b {
		b[i] = 1
	}
	return []int{int(b[7])}
}
`, StatusMLE},
		"network": {`package main

import "net/http"

func twoSum(nums []int, target int) []int {
	if _, err := http.Get("http://1.1.1.1"); err != nil {
		panic(err)
	}
	return nil
}
`, StatusRuntimeError},
		"write-src": {`package main

import "os"

func twoSum(nums []int, target int) []int {
	if err := os.WriteFile("/src/input.json", []byte("x"), 0o644); err != nil {
		panic(err)
	}
	return nil
}
`, StatusRuntimeError},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			res := Check(context.Background(), sb, Job{UserID: 1, Task: task, Lang: catalog.Go, Code: c.code})
			if res.Status != c.want {
				t.Fatalf("status %s, want %s: %+v %q", res.Status, c.want, res.Failure, res.Output)
			}
			if res.Failure != nil {
				t.Logf("%s: %s", name, res.Failure.Err)
			}
		})
	}
}

// TestReferenceSolutions checks that every task is solvable: its reference
// solution (tools/gentests/solutions, spoilers) passes all tests and its stub
// does not.
func TestReferenceSolutions(t *testing.T) {
	cat, err := catalog.Load("../..")
	if err != nil {
		t.Fatal(err)
	}
	sb := sandbox(t)
	for _, task := range cat.Tasks {
		t.Run(task.Slug, func(t *testing.T) {
			ref, err := os.ReadFile("../../tools/gentests/solutions/" + task.Slug + ".go.txt")
			if err != nil {
				t.Skip("no reference solution")
			}
			res := Check(context.Background(), sb, Job{UserID: 1, Task: task, Lang: catalog.Go, Code: string(ref), Submit: true})
			if res.Status != StatusOK {
				t.Fatalf("reference: %s %d/%d %+v %q", res.Status, res.Passed, res.Total, res.Failure, res.Output)
			}
			stub := Check(context.Background(), sb, Job{UserID: 1, Task: task, Lang: catalog.Go, Code: task.Langs[catalog.Go].Stub, Submit: true})
			if stub.Status == StatusOK {
				t.Fatal("stub passes all tests")
			}
			t.Logf("%s: ref %d/%d in %d ms", task.Slug, res.Passed, res.Total, res.TimeMS)
		})
	}
}
