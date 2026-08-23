package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/alecthomas/kong"
	"github.com/golang-jwt/jwt/v5"

	"github.com/zaolin/framework-powerd/internal/api"
	"github.com/zaolin/framework-powerd/internal/config"
	"github.com/zaolin/framework-powerd/internal/detector"
	"github.com/zaolin/framework-powerd/internal/gpu"
	"github.com/zaolin/framework-powerd/internal/monitor"
	"github.com/zaolin/framework-powerd/internal/ollama"
	"github.com/zaolin/framework-powerd/internal/power"
	"github.com/zaolin/framework-powerd/internal/smartd"
	"github.com/zaolin/framework-powerd/internal/sysinfo"
)

var CLI struct {
	Serve struct {
		Config       string        `help:"Path to config file" type:"path" name:"config" short:"c"`
		Port         int           `help:"Port to listen on" default:"8080"`
		Address      string        `help:"Address to listen on" default:"localhost"`
		JWTSecret    string        `help:"Secret key for JWT authentication" env:"JWT_SECRET" name:"jwt-secret"`
		DisableAuth  bool           `help:"Disable JWT authentication (INSECURE — exposes control plane without auth)" name:"disable-auth"`
		Debug        bool          `help:"Enable verbose logging" short:"d"`
		IdleTimeout  time.Duration `help:"Time before entering idle mode" default:"5m"`
	} `cmd:"" help:"Start the power daemon"`

	Token struct {
		Secret string `help:"Secret key used to sign the token" required:"" env:"JWT_SECRET" name:"jwt-secret" aliases:"secret"`
	} `cmd:"" help:"Generate a JWT token"`
}

func main() {
	// Disable timestamps in logging as systemd/journald handles them
	log.SetFlags(0)

	ctx := kong.Parse(&CLI)

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
	// You might want to add claims like "exp" here, but for a simple daemon maybe indefinite or long lived?
	// Let's add a reasonable default expiration (e.g., 1 year) or make it optional.
	// For "how to use it", simpler is better.

	// Create a map to store our claims
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

func runServer() {
	// Load configuration
	cfg, err := config.Load(CLI.Serve.Config)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// CLI flags override config file
	if CLI.Serve.Address != "localhost" {
		cfg.Server.Address = CLI.Serve.Address
	}
	if CLI.Serve.Port != 8080 {
		cfg.Server.Port = CLI.Serve.Port
	}
	if CLI.Serve.JWTSecret != "" {
		cfg.Server.JWTSecret = CLI.Serve.JWTSecret
	}
	if CLI.Serve.IdleTimeout != 5*time.Minute {
		cfg.Server.IdleTimeout = CLI.Serve.IdleTimeout.String()
	}

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

	if err := pm.ValidateTools(); err != nil {
		log.Fatalf("Dependencies missing: %v", err)
	}

	// Initial Default State
	log.Println("Starting Framework Power Daemon...")
	if err := pm.SetDefaultActive(); err != nil {
		log.Printf("Initial detection failed: %v\n", err)
	}

	// Components
	pauser := power.NewProcessPauser()

	// Guarded shared state. Written from multiple goroutines
	// (idle monitor, remote-play detector, steam detector, API handler)
	// and read in updatePowerState. All access goes through the lock.
	state := &daemonState{}

	// Logic handler
	updatePowerState := func() {
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
	idleMon := monitor.NewIdleMonitor(CLI.Serve.IdleTimeout, CLI.Serve.Debug)
	pm.SetIdleMonitor(idleMon)

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
	pwrCtx, pwrCancel := context.WithCancel(context.Background())
	defer pwrCancel()

	// Run turbostat in background
	go powerMon.Start(pwrCtx)

	// Start Ollama Monitor (if enabled)
	var ollamaMonitor *ollama.Monitor
	if cfg.Ollama.Enabled {
		log.Println("Ollama monitoring enabled")
		ollamaMonitor = ollama.NewMonitor(pm, powerMon, cfg.Ollama, cfg.Pricing)
		ollamaCtx, ollamaCancel := context.WithCancel(context.Background())
		defer ollamaCancel()
		go func() {
			if err := ollamaMonitor.Start(ollamaCtx); err != nil && ollamaCtx.Err() == nil {
				log.Printf("Ollama monitor error: %v", err)
			}
		}()
	}

	// Start Smartd Monitor (if enabled)
	var smartdMonitor *smartd.Monitor
	if cfg.Smartd.Enabled {
		log.Println("Smartd monitoring enabled")
		smartdMonitor = smartd.NewMonitor(cfg.Smartd)
		smartdCtx, smartdCancel := context.WithCancel(context.Background())
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

	apiServer := api.NewServer(pm, powerMon, jwtSecret, ollamaMonitor, smartdMonitor, gpuMonitor, sysInfo)

	mux := http.NewServeMux()

	// Rate-limited + auth-wrapped handlers for the control plane.
	modeHandler := apiServer.AuthMiddleware(apiServer.RateLimit(apiServer.HandleMode))
	activityHandler := apiServer.AuthMiddleware(apiServer.RateLimit(apiServer.HandleActivity))
	statusHandler := apiServer.AuthMiddleware(apiServer.HandleStatus)
	statsHandler := apiServer.AuthMiddleware(apiServer.HandleOllamaStats)
	smartdHandler := apiServer.AuthMiddleware(apiServer.HandleSmartdStats)

	// When auth is disabled (--disable-auth), AuthMiddleware passes through.
	if authEnabled {
		log.Println("JWT Authentication enabled")
	} else {
		log.Println("WARNING: Authentication disabled. API is unauthenticated.")
	}

	mux.HandleFunc("/mode", modeHandler)
	mux.HandleFunc("/status", statusHandler)
	mux.HandleFunc("/activity", activityHandler)
	mux.HandleFunc("/ollama/stats", statsHandler)
	mux.HandleFunc("/smartd/stats", smartdHandler)
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

	// R2+R3: Graceful shutdown. Run the server in a goroutine; on signal,
	// call srv.Shutdown with a bounded timeout instead of log.Fatalf.
	serverErr := make(chan error, 1)
	go func() {
		log.Printf("Listening on %s...\n", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		log.Printf("HTTP server error: %v", err)
	case <-stop:
		log.Println("Shutting down...")
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

func (s *daemonState) setIdle(v bool)         { s.mu.Lock(); s.isIdle = v; s.mu.Unlock() }
func (s *daemonState) setRemotePlay(v bool)   { s.mu.Lock(); s.isRemotePlay = v; s.mu.Unlock() }
func (s *daemonState) setGame(pid int)        { s.mu.Lock(); s.isGameRunning = pid > 0; s.gamePID = pid; s.mu.Unlock() }

// powerDecision is the outcome of the state machine priority table.
// It tells the caller what actions to take, not how to take them — so the
// decision logic can be table-tested without real PowerManager/Pauser.
type powerDecision struct {
	shouldIdle     bool
	shouldPause    bool
	shouldResume   bool
	shouldPerf     bool
	shouldDefault  bool
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
