package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/alecthomas/kong"
	"github.com/golang-jwt/jwt/v5"

	"github.com/zaolin/framework-powerd/internal/api"
	"github.com/zaolin/framework-powerd/internal/config"
	"github.com/zaolin/framework-powerd/internal/detector"
	"github.com/zaolin/framework-powerd/internal/monitor"
	"github.com/zaolin/framework-powerd/internal/ollama"
	"github.com/zaolin/framework-powerd/internal/power"
)

var CLI struct {
	Serve struct {
		Config      string        `help:"Path to config file" type:"path" name:"config" short:"c"`
		Port        int           `help:"Port to listen on" default:"8080"`
		Address     string        `help:"Address to listen on" default:"localhost"`
		JWTSecret   string        `help:"Secret key for JWT authentication" env:"JWT_SECRET" name:"jwt-secret"`
		Debug       bool          `help:"Enable verbose logging" short:"d"`
		IdleTimeout time.Duration `help:"Time before entering idle mode" default:"5m"`
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

	pm := power.NewPowerManager()

	if err := pm.ValidateTools(); err != nil {
		log.Fatalf("Dependencies missing: %v", err)
	}

	// Initial Default State
	log.Println("Starting Framework Power Daemon...")
	if err := pm.SetDefaultActive(); err != nil {
		log.Printf("Initial detection failed: %v\n", err)
	}

	// State tracking
	var isGameRunning bool
	var gamePID int
	var isIdle bool
	var isRemotePlay bool // Used mainly for logging now, as idle handles "active" remote play

	// Components
	pauser := power.NewProcessPauser()

	// Logic handler
	updatePowerState := func() {

		// New Priority:
		// 1. Idle -> Power Saver (AND Pause Game if running)
		// 2. Active (Input Detected) -> Performance (AND Resume Game if running)

		// Refined Priority based on User Feedback:
		// "remote play and a game is active then ignore idle"

		shouldIdle := isIdle

		if isRemotePlay && isGameRunning {
			if isIdle {
				log.Println("[PowerLogic] System Idle, but Remote Play & Game Active. Keeping Performance.")
			}
			shouldIdle = false
		}

		if shouldIdle {
			log.Println("[PowerLogic] System Idle (No Input). Force Power Saver.")

			// Pause Game if running
			if isGameRunning && gamePID > 0 {
				if !pauser.IsPaused() {
					if err := pauser.Pause(gamePID); err != nil {
						log.Printf("Failed to pause game: %v\n", err)
					}
				}
			}

			if err := pm.SetPowersave("System Idle"); err != nil {
				log.Printf("Failed to set powersave: %v\n", err)
			}
		} else {
			// System Active
			// Resume Game if paused
			if pauser.IsPaused() {
				if err := pauser.Resume(); err != nil {
					log.Printf("Failed to resume game: %v\n", err)
				}
			}

			if isGameRunning || isRemotePlay {
				log.Println("[PowerLogic] Active Usage (Game/Remote). Force Performance.")
				if err := pm.SetPerformance("Game/Remote Active"); err != nil {
					log.Printf("Failed to set performance: %v\n", err)
				}
			} else {
				log.Println("[PowerLogic] Active Usage (Desktop). Set Default Active.")
				if err := pm.SetDefaultActive(); err != nil {
					log.Printf("Failed to set default active: %v\n", err)
				}
			}
		}

		// Update Status within (potentially) new state
		pm.SetState(isIdle, isRemotePlay, isGameRunning, gamePID, pauser.IsPaused())
	}

	// Start Idle Monitor (Raw Input)
	idleMon := monitor.NewIdleMonitor(CLI.Serve.IdleTimeout, CLI.Serve.Debug)
	pm.SetIdleMonitor(idleMon)

	idleCtx, idleCancel := context.WithCancel(context.Background())
	defer idleCancel()

	go func() {
		if err := idleMon.Start(idleCtx,
			func() {
				isIdle = true
				updatePowerState()
			},
			func() {
				isIdle = false
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
				isRemotePlay = true
				// Note: Remote Play creates a virtual input device.
				// We now explicitly add these devices to the IdleMonitor logic to ensure
				// they are watched, even if hotplug detection missed them or they are unusual.
				for _, dev := range devices {
					idleMon.AddDevice(idleCtx, dev)
				}
				updatePowerState()
			},
			func() {
				isRemotePlay = false
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
		isGameRunning = true
		gamePID = initialPID
		steamDet.LastPID = initialPID
		pauser.SyncState(initialPID)
		// No need to call updatePowerState() here, it will be called by idleMon or rpDet callbacks
		// or we can force it:
		pm.SetState(isIdle, isRemotePlay, isGameRunning, gamePID, pauser.IsPaused())
	}

	// Create a context for the detector
	detCtx, detCancel := context.WithCancel(context.Background())
	defer detCancel()

	go steamDet.Start(detCtx,
		func(pid int) {
			// On Game Start
			log.Printf("Steam Game Started (PID: %d).\n", pid)
			isGameRunning = true
			gamePID = pid

			// Check if it's already paused (e.g. restarts)
			pauser.SyncState(pid)

			updatePowerState()
		},
		func() {
			// On Game Stop
			log.Println("Steam Game Stopped.")
			isGameRunning = false
			gamePID = 0
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

	// Start API Server
	jwtSecret := strings.TrimSpace(cfg.Server.JWTSecret)
	apiServer := api.NewServer(pm, powerMon, jwtSecret, ollamaMonitor)

	// Apply middleware if secret is set
	if jwtSecret != "" {
		log.Println("JWT Authentication enabled")
		http.HandleFunc("/mode", apiServer.AuthMiddleware(apiServer.HandleMode))
		http.HandleFunc("/status", apiServer.AuthMiddleware(apiServer.HandleStatus))
		http.HandleFunc("/activity", apiServer.AuthMiddleware(apiServer.HandleActivity))
		http.HandleFunc("/ollama/stats", apiServer.AuthMiddleware(apiServer.HandleOllamaStats))
	} else {
		log.Println("Warning: JWT Authentication disabled (no secret provided)")
		http.HandleFunc("/mode", apiServer.HandleMode)
		http.HandleFunc("/status", apiServer.HandleStatus)
		http.HandleFunc("/activity", apiServer.HandleActivity)
		http.HandleFunc("/ollama/stats", apiServer.HandleOllamaStats)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		addr := fmt.Sprintf("%s:%d", cfg.Server.Address, cfg.Server.Port)
		log.Printf("Listening on %s...\n", addr)
		if err := http.ListenAndServe(addr, nil); err != nil {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	<-stop
	log.Println("Shutting down...")
}
