package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/alecthomas/kong"
	"github.com/coreos/go-systemd/v22/daemon"
	"github.com/golang-jwt/jwt/v5"

	"github.com/zaolin/framework-powerd/internal/api"
	"github.com/zaolin/framework-powerd/internal/config"
	"github.com/zaolin/framework-powerd/internal/detector"
	"github.com/zaolin/framework-powerd/internal/gpu"
	"github.com/zaolin/framework-powerd/internal/llm"
	"github.com/zaolin/framework-powerd/internal/monitor"
	"github.com/zaolin/framework-powerd/internal/nvme"
	"github.com/zaolin/framework-powerd/internal/peripherals"
	"github.com/zaolin/framework-powerd/internal/power"
	"github.com/zaolin/framework-powerd/internal/smartd"
	"github.com/zaolin/framework-powerd/internal/sysinfo"
)

// version is injected at build time via -ldflags "-X main.version=...".
var version = "dev"

var CLI struct {
	Version kong.VersionFlag `name:"version" help:"Print version and exit"`

	Serve struct {
		Config      string         `help:"Path to config file" type:"path" name:"config" short:"c"`
		Port        *int           `help:"Port to listen on (default: 8080)"`
		Address     *string        `help:"Address to listen on (default: localhost)"`
		JWTSecret   string         `help:"Secret key for JWT authentication" env:"JWT_SECRET" name:"jwt-secret"`
		DisableAuth bool           `help:"Disable JWT authentication (INSECURE — exposes control plane without auth)" name:"disable-auth"`
		Debug       bool           `help:"Enable verbose logging" short:"d"`
		IdleTimeout *time.Duration `help:"Time before entering idle mode (default: config value or 5m)"`
	} `cmd:"" help:"Start the power daemon"`

	Token struct {
		Secret string `help:"Secret key used to sign the token" required:"" env:"JWT_SECRET" name:"jwt-secret" aliases:"secret"`
	} `cmd:"" help:"Generate a JWT token"`
}

func main() {
	// Disable timestamps in logging as systemd/journald handles them
	log.SetFlags(0)

	ctx := kong.Parse(&CLI, kong.Vars{"version": version})

	switch ctx.Command() {
	case "serve":
		runServer()
	case "token":
		generateToken()
	default:
		log.Fatal("Unknown command")
	}
}

func generateToken() {
	token := jwt.New(jwt.SigningMethodHS256)
	claims := token.Claims.(jwt.MapClaims)
	claims["authorized"] = true
	claims["exp"] = time.Now().Add(time.Hour * 24 * 365).Unix() // 1 year

	secret := strings.TrimSpace(CLI.Token.Secret)
	tokenString, err := token.SignedString([]byte(secret))
	if err != nil {
		log.Fatalf("Error generating token: %v", err)
	}

	fmt.Println(tokenString)
}

// applyCLIOverrides applies explicitly provided CLI flags on top of the
// configuration loaded from disk. Pointer flags remain nil when the user did
// not pass them, so a config value is only replaced on an explicit flag.
func applyCLIOverrides(cfg *config.Config) {
	if CLI.Serve.Address != nil {
		cfg.Server.Address = *CLI.Serve.Address
	}
	if CLI.Serve.Port != nil {
		cfg.Server.Port = *CLI.Serve.Port
	}
	if CLI.Serve.JWTSecret != "" {
		cfg.Server.JWTSecret = CLI.Serve.JWTSecret
	}
	if CLI.Serve.IdleTimeout != nil {
		cfg.Server.IdleTimeout = CLI.Serve.IdleTimeout.String()
	}
}

func runServer() {
	// Load configuration
	cfg, err := config.Load(CLI.Serve.Config)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// CLI flags override config file values. The flags are pointers so we can
	// tell "not provided" (nil) apart from an explicit value, including an
	// explicit value that happens to equal the default.
	applyCLIOverrides(cfg)

	if err := cfg.Server.Validate(); err != nil {
		log.Fatalf("Invalid configuration: %v", err)
	}
	if err := cfg.Metrics.Validate(); err != nil {
		log.Fatalf("Invalid configuration: %v", err)
	}
	if err := cfg.Smartd.Validate(); err != nil {
		log.Fatalf("Invalid configuration: %v", err)
	}
	if err := cfg.NVMe.Validate(); err != nil {
		log.Fatalf("Invalid configuration: %v", err)
	}
	if err := cfg.LLM.Validate(); err != nil {
		log.Fatalf("Invalid configuration: %v", err)
	}
	if cfg.LLM.Enabled {
		if err := cfg.Pricing.Validate(); err != nil {
			log.Fatalf("Invalid configuration: %v", err)
		}
	}

	sysinfo.DaemonVersion = version

	// S2: Fail-closed auth. The daemon controls power profiles and process
	// SIGSTOP/SIGCONT — its API is a control plane. Require an explicit opt-in
	// to run without authentication.
	jwtSecret := strings.TrimSpace(cfg.Server.JWTSecret)
	if jwtSecret == "" && !CLI.Serve.DisableAuth {
		log.Fatal("JWT secret required: set --jwt-secret (or JWT_SECRET env), or pass --disable-auth to run without authentication (INSECURE).")
	}
	authEnabled := jwtSecret != ""
	if !authEnabled {
		log.Println("WARNING: Authentication disabled via --disable-auth. The API is unauthenticated — bind to localhost only!")
	}

	pm := power.NewPowerManager()
	pm.Debug = CLI.Serve.Debug

	if err := pm.ValidateTools(); err != nil {
		log.Fatalf("Dependencies missing: %v", err)
	}

	// Initial Default State
	log.Printf("Starting Framework Power Daemon %s...", version)
	if err := pm.SetDefaultActive(); err != nil {
		log.Printf("Initial detection failed: %v\n", err)
	}

	// Components
	pauser := power.NewProcessPauser()

	// Never leave a game frozen when the daemon exits. This covers both the
	// graceful shutdown path and early returns.
	defer func() {
		if pauser.IsPaused() {
			log.Println("Releasing paused game processes before exit...")
			if err := pauser.Resume(); err != nil {
				log.Printf("Failed to resume game on shutdown: %v", err)
			}
		}
	}()

	// Guarded shared state. Written from multiple goroutines
	// (idle monitor, remote-play detector, steam detector, API handler)
	// and read in updatePowerState. All access goes through the lock.
	state := &daemonState{}

	// Serialize reconciliations: idle, remote-play, steam and API callbacks
	// must not interleave SetPerformance/SetPowersave or pause/resume.
	var reconcileMu sync.Mutex

	// Logic handler
	updatePowerState := func() {
		reconcileMu.Lock()
		defer reconcileMu.Unlock()

		st := state.snapshot()
		d := decidePowerMode(st, pauser.IsPaused())

		if d.shouldIdle {
			log.Println("[PowerLogic] System Idle (No Input). Force Power Saver.")

			if d.shouldPause {
				if err := pauser.Pause(st.gamePID); err != nil {
					log.Printf("Failed to pause game: %v\n", err)
				}
			}

			if err := pm.SetPowersave("System Idle"); err != nil {
				log.Printf("Failed to set powersave: %v\n", err)
			}
		} else {
			if d.shouldResume {
				if err := pauser.Resume(); err != nil {
					log.Printf("Failed to resume game: %v\n", err)
				}
			}

			if d.shouldPerf {
				log.Println("[PowerLogic] Active Usage (Game/Remote). Force Performance.")
				if err := pm.SetPerformance("Game/Remote Active"); err != nil {
					log.Printf("Failed to set performance: %v\n", err)
				}
			} else if d.shouldDefault {
				log.Println("[PowerLogic] Active Usage (Desktop). Set Default Active.")
				if err := pm.SetDefaultActive(); err != nil {
					log.Printf("Failed to set default active: %v\n", err)
				}
			}
		}

		// Update Status within (potentially) new state
		pm.SetState(st.isIdle, st.isRemotePlay, st.isGameRunning, st.gamePID, pauser.IsPaused())
	}

	// Start Idle Monitor (Raw Input)
	idleTimeout := cfg.Server.GetIdleTimeout()
	idleMon := monitor.NewIdleMonitor(idleTimeout, CLI.Serve.Debug)
	pm.SetIdleMonitor(idleMon)
	log.Printf("Idle timeout: %s", idleTimeout)

	idleCtx, idleCancel := context.WithCancel(context.Background())
	defer idleCancel()

	go func() {
		if err := idleMon.Start(idleCtx,
			func() {
				state.setIdle(true)
				updatePowerState()
			},
			func() {
				state.setIdle(false)
				updatePowerState()
			},
		); err != nil {
			log.Printf("Idle monitor failed: %v\n", err)
		}
	}()

	// Start Steam Remote Play Detector
	rpDet := detector.NewRemotePlayDetector()
	rpCtx, rpCancel := context.WithCancel(context.Background())
	defer rpCancel()

	go func() {
		if err := rpDet.Start(rpCtx,
			func(devices []string) {
				state.setRemotePlay(true)
				// Note: Remote Play creates a virtual input device.
				// We now explicitly add these devices to the IdleMonitor logic to ensure
				// they are watched, even if hotplug detection missed them or they are unusual.
				for _, dev := range devices {
					idleMon.AddDevice(idleCtx, dev)
				}
				updatePowerState()
			},
			func() {
				state.setRemotePlay(false)
				updatePowerState()
			},
		); err != nil {
			log.Printf("Remote Play detector failed: %v\n", err)
		}
	}()

	// Start Steam Game Detector
	// Poll every 5 seconds
	steamDet := detector.NewSteamDetector(5 * time.Second)

	// Single Synchronous Detection on Startup
	// This ensures we catch any running/paused game immediately even after daemon restart
	if initialPID, err := steamDet.Detect(); err == nil && initialPID > 0 {
		log.Printf("[Startup] Detected existing Steam Game (PID: %d)\n", initialPID)
		state.setGame(initialPID)
		steamDet.LastPID = initialPID
		pauser.SyncState(initialPID)
		// No need to call updatePowerState() here, it will be called by idleMon or rpDet callbacks
		// or we can force it:
		st := state.snapshot()
		pm.SetState(st.isIdle, st.isRemotePlay, st.isGameRunning, st.gamePID, pauser.IsPaused())
	}

	// Create a context for the detector
	detCtx, detCancel := context.WithCancel(context.Background())
	defer detCancel()

	go steamDet.Start(detCtx,
		func(pid int) {
			// On Game Start
			log.Printf("Steam Game Started (PID: %d).\n", pid)
			state.setGame(pid)

			// Check if it's already paused (e.g. restarts)
			pauser.SyncState(pid)

			updatePowerState()
		},
		func() {
			// On Game Stop
			log.Println("Steam Game Stopped.")
			state.setGame(0)
			// Ensure we resume if we were paused (though process is likely gone/stopping)
			if pauser.IsPaused() {
				pauser.Resume()
			}
			updatePowerState()
		},
	)

	// Start Power Monitor
	powerMon := power.NewPowerMonitor()

	// Auto-detect peripherals (USB, NVMe, fan, WiFi, Ethernet) and attach
	// the estimator + mode provider to the power monitor.
	periphEst := peripherals.Detect(peripherals.Config{
		VRMLossPercent:  cfg.Peripherals.VRMLossPercent,
		FanIdleRPM:      cfg.Peripherals.FanIdleRPM,
		NVMeIdleWatts:   cfg.Peripherals.NVMeIdleWatts,
		NVMeActiveWatts: cfg.Peripherals.NVMeActiveWatts,
		WiFiIdleWatts:   cfg.Peripherals.WiFiIdleWatts,
		WiFiActiveWatts: cfg.Peripherals.WiFiActiveWatts,
	})
	periphEst.LogDetection()
	powerMon.SetPeripherals(periphEst)
	// Peripheral estimation needs the active/idle tunable state, not the
	// reported "balanced" profile — see CurrentTunableState.
	powerMon.SetModeProvider(pm.CurrentTunableState)

	// Restore aggregated energy metrics before monitoring starts.
	if cfg.Metrics.Persist {
		powerMon.SetStatePath(cfg.Metrics.StateFile)
		powerMon.SetSaveInterval(cfg.Metrics.GetSaveInterval())
		if restored, err := powerMon.LoadState(); err != nil {
			log.Printf("[PowerMonitor] Could not restore metrics state: %v", err)
		} else if restored {
			log.Println("[PowerMonitor] Restored aggregated energy metrics.")
		}
	} else {
		powerMon.SetStatePath("")
	}

	pwrCtx, pwrCancel := context.WithCancel(context.Background())
	defer pwrCancel()

	// Run turbostat in background
	go powerMon.Start(pwrCtx)
	powerMon.StartPersistence(pwrCtx)

	// Start Smartd Monitor (if enabled)
	var smartdMonitor *smartd.Monitor
	var smartdCancel context.CancelFunc
	if cfg.Smartd.Enabled {
		log.Println("Smartd monitoring enabled")
		smartdMonitor = smartd.NewMonitor(cfg.Smartd)
		if restored, err := smartdMonitor.LoadState(); err != nil {
			log.Printf("[SmartdMonitor] Could not restore alerts: %v", err)
		} else if restored {
			log.Println("[SmartdMonitor] Restored persisted alerts.")
		}
		smartdCtx, cancel := context.WithCancel(context.Background())
		smartdCancel = cancel
		defer smartdCancel()
		go func() {
			if err := smartdMonitor.Start(smartdCtx); err != nil {
				if smartdCtx.Err() == context.Canceled {
					log.Printf("[SmartdMonitor] Shutdown complete")
				} else {
					log.Printf("[SmartdMonitor] Error: %v", err)
				}
			}
		}()
	}

	// Start direct NVMe health polling (if enabled)
	var nvmeMonitor *nvme.Monitor
	if cfg.NVMe.Enabled {
		log.Println("NVMe health polling enabled")
		nvmeMonitor = nvme.NewMonitor(cfg.NVMe)
		nvmeCtx, nvmeCancel := context.WithCancel(context.Background())
		defer nvmeCancel()
		go nvmeMonitor.Start(nvmeCtx)
	}

	// Start LLM request accounting (if enabled)
	var llmMonitor *llm.Monitor
	var llmCancel context.CancelFunc
	var llmDone chan struct{}
	var llmPersistDone chan struct{}
	if cfg.LLM.Enabled {
		log.Println("LLM request accounting enabled")
		llmMonitor = llm.NewMonitor(cfg.LLM, cfg.Pricing)
		llmMonitor.SetPowerProvider(func() float64 {
			return powerMon.GetStatus().Current.EstimatedTotalWatts
		})
		if restored, err := llmMonitor.LoadState(); err != nil {
			log.Printf("[LLM] Could not restore state: %v", err)
		} else if restored {
			log.Println("[LLM] Restored request accounting state.")
		}
		var llmCtx context.Context
		llmCtx, llmCancel = context.WithCancel(context.Background())
		defer llmCancel()
		llmDone = make(chan struct{})
		llmPersistDone = make(chan struct{})
		go func() {
			defer close(llmDone)
			llmMonitor.Start(llmCtx)
		}()
		go func() {
			defer close(llmPersistDone)
			llmMonitor.StartPersistence(llmCtx)
		}()
	}

	// Start GPU Monitor (if enabled)
	var gpuMonitor *gpu.Monitor
	if cfg.GPU.Enabled {
		log.Println("GPU monitoring enabled")
		var err error
		gpuMonitor, err = gpu.NewMonitor(cfg.GPU)
		if err != nil {
			log.Printf("GPU monitor initialization failed: %v", err)
		} else {
			gpuCtx, gpuCancel := context.WithCancel(context.Background())
			defer gpuCancel()
			go func() {
				if err := gpuMonitor.Start(gpuCtx); err != nil {
					if gpuCtx.Err() == context.Canceled {
						log.Printf("[GPUMonitor] Shutdown complete")
					} else {
						log.Printf("[GPUMonitor] Error: %v", err)
					}
				}
			}()
		}
	}

	// Start API Server (S3+S8: dedicated mux, explicit timeouts, body limits)
	// Collect static system info once at startup.
	sysInfo := sysinfo.Collect()

	apiServer := api.NewServer(pm, powerMon, jwtSecret, smartdMonitor, nvmeMonitor, llmMonitor, gpuMonitor, sysInfo)

	mux := http.NewServeMux()

	// Rate-limited + auth-wrapped handlers for the control plane.
	modeHandler := apiServer.AuthMiddleware(apiServer.RateLimit(apiServer.HandleMode))
	activityHandler := apiServer.AuthMiddleware(apiServer.RateLimit(apiServer.HandleActivity))
	statusHandler := apiServer.AuthMiddleware(apiServer.HandleStatus)
	statsHandler := apiServer.AuthMiddleware(apiServer.HandleSmartdStats)
	nvmeHandler := apiServer.AuthMiddleware(apiServer.HandleNVMeStats)
	llmHandler := apiServer.AuthMiddleware(apiServer.HandleLLMStats)

	// When auth is disabled (--disable-auth), AuthMiddleware passes through.
	if authEnabled {
		log.Println("JWT Authentication enabled")
	} else {
		log.Println("WARNING: Authentication disabled. API is unauthenticated.")
	}

	// /healthz is intentionally unauthenticated and read-only: it exposes no
	// control plane and is safe for systemd/monitoring probes.
	mux.HandleFunc("/healthz", apiServer.HandleHealth)
	mux.HandleFunc("/mode", modeHandler)
	mux.HandleFunc("/status", statusHandler)
	mux.HandleFunc("/activity", activityHandler)
	mux.HandleFunc("/smartd/stats", statsHandler)
	mux.HandleFunc("/nvme/stats", nvmeHandler)
	mux.HandleFunc("/llm/stats", llmHandler)
	// S8: 404 catch-all for unmatched paths.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	addr := fmt.Sprintf("%s:%d", cfg.Server.Address, cfg.Server.Port)
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second, // S3: slowloris protection
		ReadTimeout:       20 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// Bind explicitly so startup errors are reported before READY=1.
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("Failed to listen on %s: %v", addr, err)
	}

	serverErr := make(chan error, 1)
	go func() {
		log.Printf("Listening on %s...\n", addr)
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	// Tell systemd we are ready. No-op when not running under systemd.
	if sent, err := daemon.SdNotify(false, daemon.SdNotifyReady); err != nil {
		log.Printf("sd_notify READY failed: %v", err)
	} else if sent {
		log.Println("Notified systemd: READY=1")
	}

	// Watchdog pings. No-op when WatchdogSec is not configured.
	shutdownStarted := make(chan struct{})
	if wd, err := daemon.SdWatchdogEnabled(false); err == nil && wd > 0 {
		go func() {
			ticker := time.NewTicker(wd / 2)
			defer ticker.Stop()
			for {
				select {
				case <-shutdownStarted:
					return
				case <-ticker.C:
					_, _ = daemon.SdNotify(false, daemon.SdNotifyWatchdog)
				}
			}
		}()
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		log.Printf("HTTP server error: %v", err)
	case <-stop:
		log.Println("Shutting down...")
	}

	close(shutdownStarted)
	_, _ = daemon.SdNotify(false, daemon.SdNotifyStopping)

	// Stop producing new power samples, then flush the final metrics state.
	pwrCancel()
	if err := powerMon.SaveState(); err != nil {
		log.Printf("Failed to save metrics state on shutdown: %v", err)
	}

	// Flush sticky SMART alerts before exit.
	if smartdMonitor != nil {
		if smartdCancel != nil {
			smartdCancel()
		}
		if err := smartdMonitor.SaveState(); err != nil {
			log.Printf("Failed to save SMART alerts on shutdown: %v", err)
		}
	}

	// Stop the LLM poller and persistence loop before flushing, so the saved
	// snapshot is final.
	if llmMonitor != nil {
		if llmCancel != nil {
			llmCancel()
		}
		<-llmDone
		<-llmPersistDone
		if err := llmMonitor.SaveState(); err != nil {
			log.Printf("Failed to save LLM state on shutdown: %v", err)
		}
	}

	// Give the HTTP server up to 5 seconds to finish in-flight requests.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP shutdown error: %v", err)
	}
}

// daemonState is the shared, mutex-guarded state written from multiple
// goroutines (idle monitor, remote-play detector, steam detector, API
// handler via TriggerActivity). Without this guard the four bare vars that
// preceded it raced under -race (A2). All access goes through the methods.
type daemonState struct {
	mu            sync.Mutex
	isIdle        bool
	isRemotePlay  bool
	isGameRunning bool
	gamePID       int
}

// stateSnapshot is a consistent point-in-time copy of daemonState.
type stateSnapshot struct {
	isIdle        bool
	isRemotePlay  bool
	isGameRunning bool
	gamePID       int
}

func (s *daemonState) snapshot() stateSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return stateSnapshot{
		isIdle:        s.isIdle,
		isRemotePlay:  s.isRemotePlay,
		isGameRunning: s.isGameRunning,
		gamePID:       s.gamePID,
	}
}

func (s *daemonState) setIdle(v bool)       { s.mu.Lock(); s.isIdle = v; s.mu.Unlock() }
func (s *daemonState) setRemotePlay(v bool) { s.mu.Lock(); s.isRemotePlay = v; s.mu.Unlock() }
func (s *daemonState) setGame(pid int) {
	s.mu.Lock()
	s.isGameRunning = pid > 0
	s.gamePID = pid
	s.mu.Unlock()
}

// powerDecision is the outcome of the state machine priority table.
// It tells the caller what actions to take, not how to take them — so the
// decision logic can be table-tested without real PowerManager/Pauser.
type powerDecision struct {
	shouldIdle    bool
	shouldPause   bool
	shouldResume  bool
	shouldPerf    bool
	shouldDefault bool
}

// decidePowerMode encodes the 4-branch priority table extracted from
// updatePowerState (T4). It takes only immutable state and returns the
// action set, making it pure and table-testable.
//
// Priority:
// 1. Remote Play + Game running → never idle (force performance).
// 2. Idle → powersave + pause game.
// 3. Active + Game/Remote → performance + resume game.
// 4. Active + Desktop → default active + resume game.
func decidePowerMode(st stateSnapshot, isPaused bool) powerDecision {
	shouldIdle := st.isIdle

	// "remote play and a game is active then ignore idle"
	if st.isRemotePlay && st.isGameRunning {
		shouldIdle = false
	}

	d := powerDecision{shouldIdle: shouldIdle}

	if shouldIdle {
		d.shouldPause = st.isGameRunning && st.gamePID > 0 && !isPaused
		// SetPowersave is always called when idle.
	} else {
		d.shouldResume = isPaused
		if st.isGameRunning || st.isRemotePlay {
			d.shouldPerf = true
		} else {
			d.shouldDefault = true
		}
	}

	return d
}
