package judge

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/Virviil/drill/internal/catalog"
)

// Images per language for the Docker sandbox. Built from sandbox/*.Dockerfile.
var Images = map[catalog.Lang]string{
	catalog.Go: "drill-go:1",
}

const compileTimeout = 30 * time.Second

// Script run inside the container. $1 = run timeout in seconds.
// Build output goes to stderr; exit 101 means "did not compile".
var scripts = map[catalog.Lang]string{
	catalog.Go: `set -u
mkdir -p /tmp/w && cp /src/*.go /src/go.mod /tmp/w/ && cd /tmp/w
if ! go build -o /tmp/w/sol . 2>/tmp/w/build.log; then
  sed 's#^\./##' /tmp/w/build.log >&2
  exit 101
fi
timeout "$1" /tmp/w/sol < /src/input.json
exit $?`,
}

// Docker runs each job in a fresh container: no network, capped memory,
// CPU, pids, unprivileged user, no capabilities.
type Docker struct {
	Binary   string // docker or podman
	MemoryMB int
	CPUs     string
	Runtime  string // optional, e.g. runsc for gVisor
}

func (d Docker) Run(ctx context.Context, lang catalog.Lang, dir string, timeLimitMS int) ([]byte, []byte, int, error) {
	image, ok := Images[lang]
	if !ok {
		return nil, nil, 0, fmt.Errorf("нет образа для %s", lang)
	}
	runSec := (timeLimitMS + 999) / 1000
	ctx, cancel := context.WithTimeout(ctx, compileTimeout+time.Duration(runSec+10)*time.Second)
	defer cancel()

	name := fmt.Sprintf("drill-job-%d", time.Now().UnixNano())
	mem := fmt.Sprintf("%dm", d.MemoryMB)
	args := []string{"run", "--rm", "-i", "--name", name,
		"--network=none",
		"--memory=" + mem, "--memory-swap=" + mem,
		"--cpus=" + d.CPUs,
		"--pids-limit=128",
		"--cap-drop=ALL",
		"--security-opt=no-new-privileges",
		"--user=65534:65534",
		"--tmpfs=/tmp:rw,exec,size=256m",
		"-v", dir + ":/src:ro",
	}
	if d.Runtime != "" {
		args = append(args, "--runtime="+d.Runtime)
	}
	args = append(args, image, "sh", "-c", scripts[lang], "sh", strconv.Itoa(runSec))

	cmd := exec.CommandContext(ctx, d.Binary, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &limitWriter{w: &stdout, n: 64 << 20}
	cmd.Stderr = &limitWriter{w: &stderr, n: 64 << 10}
	err := cmd.Run()
	if ctx.Err() != nil {
		// Make sure the container does not outlive us.
		exec.Command(d.Binary, "rm", "-f", name).Run()
		return stdout.Bytes(), stderr.Bytes(), exitTimeout, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code := ee.ExitCode()
		if code == 125 { // docker itself failed
			return nil, nil, 0, fmt.Errorf("docker: %s", stderr.String())
		}
		return stdout.Bytes(), stderr.Bytes(), code, nil
	}
	if err != nil {
		return nil, nil, 0, err
	}
	return stdout.Bytes(), stderr.Bytes(), 0, nil
}

// Local builds and runs on the host with no isolation. Development only.
type Local struct{}

func (Local) Run(ctx context.Context, lang catalog.Lang, dir string, timeLimitMS int) ([]byte, []byte, int, error) {
	if lang != catalog.Go {
		return nil, nil, 0, fmt.Errorf("local sandbox supports only go")
	}
	bctx, cancel := context.WithTimeout(ctx, compileTimeout)
	defer cancel()
	bin := filepath.Join(dir, "sol")
	build := exec.CommandContext(bctx, "go", "build", "-o", bin, ".")
	build.Dir = dir
	build.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOFLAGS=-mod=mod")
	if out, err := build.CombinedOutput(); err != nil {
		return nil, bytes.ReplaceAll(out, []byte("./"), nil), exitCompile, nil
	}
	rctx, cancel2 := context.WithTimeout(ctx, time.Duration(timeLimitMS)*time.Millisecond)
	defer cancel2()
	in, err := os.Open(filepath.Join(dir, "input.json"))
	if err != nil {
		return nil, nil, 0, err
	}
	defer in.Close()
	run := exec.CommandContext(rctx, bin)
	run.Stdin = in
	var stdout, stderr bytes.Buffer
	run.Stdout = &limitWriter{w: &stdout, n: 64 << 20}
	run.Stderr = &limitWriter{w: &stderr, n: 64 << 10}
	err = run.Run()
	if rctx.Err() == context.DeadlineExceeded {
		return stdout.Bytes(), stderr.Bytes(), exitTimeout, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return stdout.Bytes(), stderr.Bytes(), ee.ExitCode(), nil
	}
	return stdout.Bytes(), stderr.Bytes(), 0, err
}

// limitWriter silently drops bytes past n.
type limitWriter struct {
	w *bytes.Buffer
	n int
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if rem := l.n - l.w.Len(); rem > 0 {
		if len(p) > rem {
			l.w.Write(p[:rem])
		} else {
			l.w.Write(p)
		}
	}
	return len(p), nil
}
