package fan

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// toolTimeout bounds a single framework_tool invocation. A wedged EC must not
// stall the poll loop or the shutdown path.
const toolTimeout = 5 * time.Second

// runner abstracts process execution so the EC wrapper can be tested.
type runner interface {
	CombinedOutput(name string, args ...string) ([]byte, error)
	LookPath(name string) bool
}

type realRunner struct{}

func (realRunner) CombinedOutput(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), toolTimeout)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func (realRunner) LookPath(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// EC drives the Framework embedded controller through framework_tool.
type EC struct {
	tool   string
	runner runner

	mu     sync.Mutex
	driver string // remembered working driver, "" for the default
	root   bool
}

func newEC(r runner) *EC {
	return &EC{tool: "framework_tool", runner: r, root: os.Geteuid() == 0}
}

// Available reports whether fan control is possible: the tool must be
// installed and the process must be root. framework_tool exits 0 while
// printing "Must be root" when unprivileged, so privilege is checked here
// rather than inferred from the exit status.
func (e *EC) Available() bool {
	return e.root && e.runner.LookPath(e.tool)
}

// SetDuty sets the fan duty in percent.
func (e *EC) SetDuty(percent int) error {
	if percent < 0 || percent > 100 {
		return fmt.Errorf("fan: duty %d out of range", percent)
	}
	_, err := e.run("--fansetduty", strconv.Itoa(percent))
	return err
}

// Auto returns the fan to the EC curve.
func (e *EC) Auto() error {
	_, err := e.run("--autofanctrl")
	return err
}

// run executes the tool, retrying with the cros-ec driver when the default
// driver fails. framework_tool defaults to portio, which works on the Desktop,
// but cros-ec is the fallback.
func (e *EC) run(args ...string) ([]byte, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	call := func(driver string) ([]byte, error) {
		if driver == "" {
			return e.runner.CombinedOutput(e.tool, args...)
		}
		return e.runner.CombinedOutput(e.tool, append([]string{"--driver", driver}, args...)...)
	}

	check := func(out []byte, err error) ([]byte, error) {
		if err != nil {
			return out, err
		}
		// The tool reports several failures only in its output and still exits 0.
		text := strings.ToLower(string(out))
		for _, marker := range []string{
			"must be root",
			"failed",
			"error",
			"doesn't look like",
			"empty ec response",
			"no framework ec",
		} {
			if strings.Contains(text, marker) {
				return out, fmt.Errorf("fan: framework_tool failed: %s", firstLine(string(out)))
			}
		}
		return out, nil
	}

	if e.driver != "" {
		return check(call(e.driver))
	}
	out, err := call("")
	if err == nil {
		if out, err = check(out, nil); err == nil {
			return out, nil
		}
	}
	out, retryErr := call("cros-ec")
	if retryErr != nil {
		if err != nil {
			return out, fmt.Errorf("%v; cros-ec: %w", err, retryErr)
		}
		return out, retryErr
	}
	if out, retryErr = check(out, nil); retryErr != nil {
		if err != nil {
			return out, fmt.Errorf("%v; cros-ec: %w", err, retryErr)
		}
		return out, retryErr
	}
	e.driver = "cros-ec"
	return out, nil
}

// firstLine returns the first non-empty line of the tool output.
func firstLine(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) != "" {
			return strings.TrimSpace(line)
		}
	}
	return ""
}
