package led

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zaolin/framework-powerd/internal/config"
)

type fakeRunner struct {
	available bool
	err       error
	output    string
	outputFor func([]string) (string, error)
	hook      func([]string)
	calls     [][]string
}

func (f *fakeRunner) Output(_ context.Context, _ string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string(nil), args...))
	if f.hook != nil {
		f.hook(args)
	}
	if f.outputFor != nil {
		output, err := f.outputFor(args)
		return []byte(output), err
	}
	return []byte(f.output), f.err
}

func (f *fakeRunner) LookPath(string) bool { return f.available }

func TestOffDoesNotWriteOnStartup(t *testing.T) {
	runner := &fakeRunner{available: true}
	controller := NewController(config.LEDConfig{Mode: "off", FrameInterval: "250ms"})
	controller.runner = runner

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	controller.Start(ctx)

	if len(runner.calls) != 0 {
		t.Fatalf("off must not write on startup: %v", runner.calls)
	}
	if controller.Health() != "disabled" {
		t.Errorf("health = %q, want disabled", controller.Health())
	}
}

func TestSetModeWhite(t *testing.T) {
	runner := &fakeRunner{available: true}
	controller := NewController(config.LEDConfig{Mode: "off"})
	controller.runner = runner

	if err := controller.SetMode(context.Background(), "white"); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(runner.calls))
	}
	call := runner.calls[0]
	if len(call) != 10 || call[0] != "--rgbkbd" || call[1] != "0" {
		t.Fatalf("unexpected command: %v", call)
	}
	for _, color := range call[2:] {
		if color != white {
			t.Errorf("color = %q, want white", color)
		}
	}
	if controller.Health() != "ok" {
		t.Errorf("health = %q, want ok", controller.Health())
	}
}

func TestRainbowFramesMove(t *testing.T) {
	runner := &fakeRunner{available: true}
	controller := NewController(config.LEDConfig{Mode: "off"})
	controller.runner = runner
	controller.now = func() time.Time { return time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC) }

	if err := controller.SetMode(context.Background(), "rainbow"); err != nil {
		t.Fatal(err)
	}
	if got := controller.GetStats().Frame; got != 0 {
		t.Errorf("initial frame = %d, want 0", got)
	}
	first := runner.calls[0][2:]
	for i, color := range rainbowPalette {
		if first[i] != color {
			t.Errorf("frame 0 color %d = %q, want %q", i, first[i], color)
		}
	}

	controller.writeNextFrame(context.Background())
	second := runner.calls[1][2:]
	if second[0] != "0xff1000" {
		t.Errorf("frame 1 color 0 = %q, want interpolated 0xff1000", second[0])
	}
	for i := range rainbowPalette {
		want := interpolateColor(rainbowPalette[i], rainbowPalette[(i+1)%len(rainbowPalette)], 1.0/rainbowSubsteps)
		if second[i] != want {
			t.Errorf("frame 1 color %d = %q, want %q", i, second[i], want)
		}
	}
	if half := frameColors(rainbowSubsteps / 2); half[0] != "0xff4000" {
		t.Errorf("half step = %q, want 0xff4000", half[0])
	}
	if wrapped := frameColors(rainbowFrames); wrapped[0] != rainbowPalette[0] {
		t.Errorf("wrapped frame = %q, want %q", wrapped[0], rainbowPalette[0])
	}
	if controller.GetStats().LastUpdate.IsZero() {
		t.Error("last_update must be set after a successful frame")
	}
}

func TestMissingTool(t *testing.T) {
	controller := NewController(config.LEDConfig{Mode: "off"})
	controller.runner = &fakeRunner{available: false}

	if err := controller.SetMode(context.Background(), "rainbow"); err == nil {
		t.Fatal("rainbow without framework_tool must fail")
	}
	if controller.Health() != "unsupported" {
		t.Errorf("health = %q, want unsupported", controller.Health())
	}
	if err := controller.SetMode(context.Background(), "off"); err != nil {
		t.Errorf("off should work without framework_tool: %v", err)
	}
	if controller.Health() != "disabled" {
		t.Errorf("health after off = %q, want disabled", controller.Health())
	}
}

func TestToolRequiresRoot(t *testing.T) {
	runner := &fakeRunner{available: true, output: "Must be root to access EC\n"}
	controller := NewController(config.LEDConfig{Mode: "off"})
	controller.runner = runner

	if err := controller.SetMode(context.Background(), "white"); err == nil {
		t.Fatal("a root-required response must be an error")
	}
	if controller.Health() != "error" {
		t.Errorf("health = %q, want error", controller.Health())
	}
	if !strings.Contains(controller.GetStats().LastError, "root") {
		t.Errorf("last_error = %q, want a root error", controller.GetStats().LastError)
	}
}

func TestStopTurnsOff(t *testing.T) {
	runner := &fakeRunner{available: true}
	controller := NewController(config.LEDConfig{Mode: "off"})
	controller.runner = runner

	if err := controller.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	call := runner.calls[0]
	for _, color := range call[2:] {
		if color != black {
			t.Errorf("color = %q, want black", color)
		}
	}
}

func TestInvalidMode(t *testing.T) {
	controller := NewController(config.LEDConfig{Mode: "off"})
	if err := controller.SetMode(context.Background(), "blink"); err == nil {
		t.Fatal("invalid mode must fail")
	}
}

func TestStartAppliesConfiguredWhite(t *testing.T) {
	runner := &fakeRunner{available: true}
	controller := NewController(config.LEDConfig{Mode: "white"})
	controller.runner = runner

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	controller.Start(ctx)

	if len(runner.calls) != 1 {
		t.Fatalf("calls = %d, want the initial white write", len(runner.calls))
	}
	for _, color := range runner.calls[0][2:] {
		if color != white {
			t.Errorf("color = %q, want white", color)
		}
	}
}

func TestWritesAreSerialized(t *testing.T) {
	runner := &fakeRunner{available: true}
	started := make(chan struct{})
	release := make(chan struct{})
	first := true
	runner.hook = func([]string) {
		if first {
			first = false
			close(started)
			<-release
		}
	}
	controller := NewController(config.LEDConfig{Mode: "off"})
	controller.SetRunner(runner)

	rainbowDone := make(chan error, 1)
	go func() { rainbowDone <- controller.SetMode(context.Background(), "rainbow") }()
	<-started

	offAttempted := make(chan struct{})
	offDone := make(chan error, 1)
	go func() {
		close(offAttempted)
		offDone <- controller.SetMode(context.Background(), "off")
	}()
	<-offAttempted
	close(release)

	if err := <-rainbowDone; err != nil {
		t.Fatalf("rainbow write failed: %v", err)
	}
	if err := <-offDone; err != nil {
		t.Fatalf("off write failed: %v", err)
	}
	last := runner.calls[len(runner.calls)-1][2:]
	for _, color := range last {
		if color != black {
			t.Fatalf("final write must be black after the mode change: %v", last)
		}
	}
}

func TestFailedTransitionKeepsPreviousMode(t *testing.T) {
	runner := &fakeRunner{available: true}
	runner.outputFor = func(args []string) (string, error) {
		if args[len(args)-1] == white {
			return "failed to write white", nil
		}
		return "", nil
	}
	controller := NewController(config.LEDConfig{Mode: "off"})
	controller.SetRunner(runner)

	if err := controller.SetMode(context.Background(), "rainbow"); err != nil {
		t.Fatal(err)
	}
	if err := controller.SetMode(context.Background(), "white"); err == nil {
		t.Fatal("failed white write must return an error")
	}
	if got := controller.GetStats().Mode; got != "rainbow" {
		t.Errorf("mode = %q, want previous rainbow", got)
	}
	if controller.Health() != "error" {
		t.Errorf("health = %q, want error", controller.Health())
	}
}

func TestFailedOffWriteIsHealthError(t *testing.T) {
	runner := &fakeRunner{available: true}
	runner.outputFor = func(args []string) (string, error) {
		if args[len(args)-1] == black {
			return "failed to turn off", nil
		}
		return "", nil
	}
	controller := NewController(config.LEDConfig{Mode: "off"})
	controller.SetRunner(runner)

	if err := controller.SetMode(context.Background(), "white"); err != nil {
		t.Fatal(err)
	}
	if err := controller.SetMode(context.Background(), "off"); err == nil {
		t.Fatal("failed off write must return an error")
	}
	if got := controller.GetStats().Mode; got != "white" {
		t.Errorf("mode = %q, want white", got)
	}
	if controller.Health() != "error" {
		t.Errorf("health = %q, want error", controller.Health())
	}
}

func TestDriverFallback(t *testing.T) {
	runner := &fakeRunner{available: true}
	runner.outputFor = func(args []string) (string, error) {
		if len(args) > 1 && args[0] == "--driver" && args[1] == "cros-ec" {
			return "", nil
		}
		return "failed: default driver", nil
	}
	controller := NewController(config.LEDConfig{Mode: "off"})
	controller.SetRunner(runner)

	if err := controller.SetMode(context.Background(), "white"); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("calls = %d, want default then cros-ec", len(runner.calls))
	}
	if runner.calls[1][0] != "--driver" || runner.calls[1][1] != "cros-ec" {
		t.Errorf("fallback command = %v", runner.calls[1])
	}
	if got := controller.GetStats().Driver; got != "cros-ec" {
		t.Errorf("driver = %q, want cros-ec", got)
	}
	if controller.Health() != "ok" {
		t.Errorf("health = %q, want ok", controller.Health())
	}
}

func TestSetModeRejectedAfterStop(t *testing.T) {
	runner := &fakeRunner{available: true}
	controller := NewController(config.LEDConfig{Mode: "off"})
	controller.SetRunner(runner)

	if err := controller.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	calls := len(runner.calls)
	if err := controller.SetMode(context.Background(), "white"); err == nil {
		t.Fatal("a mode change after shutdown must be rejected")
	}
	if len(runner.calls) != calls {
		t.Error("no hardware write may happen after shutdown")
	}
}

func TestStopFailureRemainsVisible(t *testing.T) {
	runner := &fakeRunner{available: true}
	runner.outputFor = func(args []string) (string, error) {
		if args[len(args)-1] == black {
			return "", errors.New("EC did not turn off")
		}
		return "", nil
	}
	controller := NewController(config.LEDConfig{Mode: "white"})
	controller.SetRunner(runner)

	if err := controller.Stop(context.Background()); err == nil {
		t.Fatal("failed shutdown write must be returned")
	}
	if controller.Health() != "error" {
		t.Errorf("health = %q, want error", controller.Health())
	}
	if err := controller.SetMode(context.Background(), "white"); err == nil {
		t.Fatal("no mode change is allowed after shutdown starts")
	}
}

func TestNoFrameAfterModeChange(t *testing.T) {
	runner := &fakeRunner{available: true}
	controller := NewController(config.LEDConfig{Mode: "off"})
	controller.SetRunner(runner)

	if err := controller.SetMode(context.Background(), "rainbow"); err != nil {
		t.Fatal(err)
	}
	if err := controller.SetMode(context.Background(), "off"); err != nil {
		t.Fatal(err)
	}
	calls := len(runner.calls)
	controller.writeNextFrame(context.Background())
	if len(runner.calls) != calls {
		t.Fatal("a queued rainbow frame must not write after off")
	}
}

func TestAdmittedRequestCannotWriteAfterStop(t *testing.T) {
	runner := &fakeRunner{available: true}
	beforeWrite := make(chan struct{})
	release := make(chan struct{})
	controller := NewController(config.LEDConfig{Mode: "off"})
	controller.SetRunner(runner)
	// Pause after the first stopping check but before taking writeMu. Stop can
	// then complete first; the admitted request must recheck and fail.
	controller.beforeWrite = func() {
		close(beforeWrite)
		<-release
	}

	whiteDone := make(chan error, 1)
	go func() { whiteDone <- controller.SetMode(context.Background(), "white") }()
	<-beforeWrite

	if err := controller.Stop(context.Background()); err != nil {
		t.Fatalf("stop failed: %v", err)
	}
	close(release)
	if err := <-whiteDone; err == nil {
		t.Error("the request admitted before shutdown must not write after Stop")
	}
	if len(runner.calls) != 1 {
		t.Fatalf("only the shutdown write is allowed: %v", runner.calls)
	}
	for _, color := range runner.calls[0][2:] {
		if color != black {
			t.Fatalf("final write must be black: %v", runner.calls[0][2:])
		}
	}
}
