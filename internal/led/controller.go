package led

import (
	"context"
	"fmt"
	"math"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zaolin/framework-powerd/internal/config"
)

var rainbowPalette = []string{
	"0xff0000",
	"0xff8000",
	"0xffff00",
	"0x00ff00",
	"0x00ffff",
	"0x0000ff",
	"0x8000ff",
	"0xff00ff",
}

const (
	black = "0x000000"
	white = "0xffffff"

	// Interpolate eight substeps between each pair of palette colors. With the
	// 50 ms default this gives a 3.2 s cycle and visibly smooth movement.
	rainbowSubsteps = 8
)

var rainbowFrames = rainbowSubsteps * len(rainbowPalette)

// CommandRunner executes framework_tool and reports availability.
type CommandRunner interface {
	Output(ctx context.Context, name string, args ...string) ([]byte, error)
	LookPath(name string) bool
}

type realRunner struct {
	timeout time.Duration
}

func (r realRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	if r.timeout <= 0 {
		r.timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func (realRunner) LookPath(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// Controller owns the keyboard backlight mode and animation loop.
type Controller struct {
	mu          sync.RWMutex
	writeMu     sync.Mutex
	cfg         config.LEDConfig
	runner      CommandRunner
	now         func() time.Time
	driver      string
	stats       Stats
	stopping    bool
	requested   bool
	beforeWrite func()
}

// NewController creates the RGB keyboard controller. Off performs no writes.
func NewController(cfg config.LEDConfig) *Controller {
	if cfg.Mode == "" {
		cfg.Mode = "off"
	}
	return &Controller{
		cfg:    cfg,
		runner: realRunner{timeout: 10 * time.Second},
		now:    time.Now,
		stats: Stats{
			Mode:            cfg.Mode,
			Supported:       false,
			FrameIntervalMS: int(cfg.GetFrameInterval().Milliseconds()),
		},
		requested: cfg.Mode != "off",
	}
}

// SetRunner replaces the framework_tool runner. It is intended for tests.
func (c *Controller) SetRunner(r CommandRunner) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.runner = r
}

// GetStats returns the current controller state.
func (c *Controller) GetStats() Stats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.stats
}

// Health reports error, off, unsupported, waiting or ok.
func (c *Controller) Health() string {
	stats := c.GetStats()
	if stats.LastError != "" {
		return "error"
	}
	if stats.Mode == "off" {
		if stats.Supported {
			return "disabled"
		}
		if c.isRequested() {
			return "unsupported"
		}
		return "disabled"
	}
	if !stats.Supported {
		return "unsupported"
	}
	if stats.LastUpdate.IsZero() {
		return "waiting"
	}
	return "ok"
}

// Start runs the animation loop. Static modes write only on mode changes.
func (c *Controller) Start(ctx context.Context) {
	if c.isStopping() {
		return
	}
	c.refreshSupport()
	switch c.GetStats().Mode {
	case "rainbow":
		_ = c.writeFrame(ctx, 0)
	case "white":
		_ = c.writeColors(ctx, repeatColor(white))
	}

	ticker := time.NewTicker(c.cfg.GetFrameInterval())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.writeNextFrame(ctx)
		}
	}
}

// SetMode selects off, white or rainbow and applies it immediately. The mode
// is committed only after the hardware write succeeds.
func (c *Controller) SetMode(ctx context.Context, mode string) error {
	switch mode {
	case "off", "white", "rainbow":
	default:
		return fmt.Errorf("mode must be off, white or rainbow, got %q", mode)
	}
	if c.isStopping() {
		return fmt.Errorf("daemon is shutting down")
	}
	if mode != "off" {
		c.mu.Lock()
		c.requested = true
		c.mu.Unlock()
	}

	c.refreshSupport()
	if c.beforeWrite != nil {
		c.beforeWrite()
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	// Recheck after acquiring the write lock: BeginShutdown may have won the
	// race while this request was waiting. No admitted request may write after
	// the final shutdown write.
	if c.isStopping() {
		return fmt.Errorf("daemon is shutting down")
	}

	if mode == "off" && !c.GetStats().Supported {
		c.mu.Lock()
		c.stats.Mode = "off"
		c.stats.LastError = ""
		c.requested = false
		c.mu.Unlock()
		return nil
	}

	var err error
	switch mode {
	case "rainbow":
		err = c.writeColorsLocked(ctx, frameColors(0))
	case "white":
		err = c.writeColorsLocked(ctx, repeatColor(white))
	case "off":
		err = c.writeColorsLocked(ctx, repeatColor(black))
	}
	if err != nil {
		return err
	}

	// Keep the hardware write and the committed mode in the same critical
	// section; otherwise a queued rainbow frame could land after off/white.
	c.mu.Lock()
	c.stats.Mode = mode
	c.requested = mode != "off"
	if mode == "rainbow" {
		c.stats.Frame = 0
	}
	c.mu.Unlock()
	return nil
}

// BeginShutdown prevents new mode changes before the final off write.
func (c *Controller) BeginShutdown() {
	c.mu.Lock()
	c.stopping = true
	c.mu.Unlock()
}

// Stop turns the keyboard off and rejects subsequent mode changes.
func (c *Controller) Stop(ctx context.Context) error {
	c.BeginShutdown()
	c.refreshSupport()
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if !c.GetStats().Supported {
		c.mu.Lock()
		c.stats.Mode = "off"
		c.stats.LastError = ""
		c.requested = false
		c.mu.Unlock()
		return nil
	}
	if err := c.writeColorsLocked(ctx, repeatColor(black)); err != nil {
		return err
	}
	c.mu.Lock()
	c.stats.Mode = "off"
	c.requested = false
	c.mu.Unlock()
	return nil
}

func (c *Controller) isRequested() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.requested
}

func (c *Controller) isStopping() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.stopping
}

func (c *Controller) refreshSupport() {
	supported := c.runner.LookPath("framework_tool")
	c.mu.Lock()
	c.stats.Supported = supported
	c.mu.Unlock()
}

func (c *Controller) writeNextFrame(ctx context.Context) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.isStopping() || c.GetStats().Mode != "rainbow" {
		return
	}

	c.mu.RLock()
	next := (c.stats.Frame + 1) % rainbowFrames
	c.mu.RUnlock()
	if err := c.writeColorsLocked(ctx, frameColors(next)); err == nil {
		c.mu.Lock()
		c.stats.Frame = next
		c.mu.Unlock()
	}
}

func (c *Controller) writeFrame(ctx context.Context, frame int) error {
	return c.writeColors(ctx, frameColors(frame))
}

func frameColors(frame int) []string {
	colors := make([]string, len(rainbowPalette))
	phase := float64(frame%rainbowFrames) / rainbowSubsteps
	for i := range colors {
		position := phase + float64(i)
		base := int(math.Floor(position)) % len(rainbowPalette)
		next := (base + 1) % len(rainbowPalette)
		blend := position - math.Floor(position)
		colors[i] = interpolateColor(rainbowPalette[base], rainbowPalette[next], blend)
	}
	return colors
}

func interpolateColor(from, to string, blend float64) string {
	if blend <= 0 {
		return strings.ToLower(from)
	}
	if blend >= 1 {
		return strings.ToLower(to)
	}
	fromR, fromG, fromB := parseColor(from)
	toR, toG, toB := parseColor(to)
	r := uint8(math.Round(float64(fromR) + (float64(toR)-float64(fromR))*blend))
	g := uint8(math.Round(float64(fromG) + (float64(toG)-float64(fromG))*blend))
	b := uint8(math.Round(float64(fromB) + (float64(toB)-float64(fromB))*blend))
	return fmt.Sprintf("0x%02x%02x%02x", r, g, b)
}

func parseColor(value string) (uint8, uint8, uint8) {
	number, err := strconv.ParseUint(strings.TrimPrefix(strings.ToLower(value), "0x"), 16, 32)
	if err != nil {
		return 0, 0, 0
	}
	return uint8(number >> 16), uint8(number >> 8), uint8(number)
}

func (c *Controller) writeColors(ctx context.Context, colors []string) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.writeColorsLocked(ctx, colors)
}

// writeColorsLocked serializes every hardware write, including the driver
// fallback. The caller must hold writeMu.
func (c *Controller) writeColorsLocked(ctx context.Context, colors []string) error {
	if !c.GetStats().Supported {
		return fmt.Errorf("framework_tool is not installed")
	}

	drivers := []string{"", "cros-ec"}
	if c.driver != "" {
		drivers = []string{c.driver, ""}
	}
	var lastErr error
	for _, driver := range drivers {
		args := append([]string(nil), colors...)
		args = append([]string{"--rgbkbd", "0"}, args...)
		if driver != "" {
			args = append([]string{"--driver", driver}, args...)
		}
		output, err := c.runner.Output(ctx, "framework_tool", args...)
		if err == nil {
			err = toolOutputError(output)
		}
		if err == nil {
			c.driver = driver
			c.mu.Lock()
			c.stats.Driver = driver
			c.stats.LastError = ""
			c.stats.LastUpdate = c.now()
			c.mu.Unlock()
			return nil
		}
		lastErr = err
	}

	c.mu.Lock()
	c.stats.LastError = lastErr.Error()
	c.mu.Unlock()
	return lastErr
}

func toolOutputError(output []byte) error {
	text := strings.TrimSpace(string(output))
	if text == "" {
		return nil
	}
	lower := strings.ToLower(text)
	for _, marker := range []string{"must be root", "failed", "error", "no framework ec"} {
		if strings.Contains(lower, marker) {
			return fmt.Errorf("framework_tool: %s", text)
		}
	}
	return nil
}

func repeatColor(color string) []string {
	colors := make([]string, len(rainbowPalette))
	for i := range colors {
		colors[i] = color
	}
	return colors
}
