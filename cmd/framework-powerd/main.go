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
	"github.com/zaolin/framework-powerd/internal/detector"
	"github.com/zaolin/framework-powerd/internal/monitor"
	"github.com/zaolin/framework-powerd/internal/power"
)

var CLI struct {
	Serve struct {
		Port      int    `help:"Port to listen on" default:"8080"`
		Address   string `help:"Address to listen on" default:"localhost"`
		JWTSecret string `help:"Secret key for JWT authentication" env:"JWT_SECRET" name:"jwt-secret"`
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
	pm := power.NewPowerManager()

	if err := pm.ValidateTools(); err != nil {
		log.Fatalf("Dependencies missing: %v", err)
	}

	// Initial Auto-Detect
	log.Println("Starting Framework Power Daemon...")
	if err := pm.AutoDetect(); err != nil {
		log.Printf("Initial auto-detect failed: %v\n", err)
	}

	// Start Udev Monitor
	udevMon, err := monitor.NewMonitor()
	if err != nil {
		log.Fatalf("Failed to start Udev monitor: %v", err)
	}
	defer udevMon.Stop()

	events, err := udevMon.Start()
	if err != nil {
		log.Fatalf("Failed to start listening to Udev events: %v", err)
	}

	go func() {
		for event := range events {
			if event.Subsystem == "drm" && event.Action == "change" {
				log.Println("Detected DRM change event, triggering auto-detect...")
				if err := pm.AutoDetect(); err != nil {
					log.Printf("Auto-detect failed: %v\n", err)
				}
			}
		}
	}()

	// State tracking
	var isGameRunning bool
	var isIdle bool
	var isRemotePlay bool

	// Start Idle Monitor (Raw Input)
	// Timeout: 5 Minutes
	idleMon := monitor.NewIdleMonitor(5 * time.Minute)

	idleCtx, idleCancel := context.WithCancel(context.Background())
	defer idleCancel()

	// Logic handler
	updatePowerState := func() {
		// Priority:
		// 1. Remote Play (Active Streaming) -> Performance (Overrides Idle)
		// 2. Idle -> Power Saver
		// 3. Game Running -> Performance
		// 4. Auto Detect

		if isRemotePlay {
			log.Println("[PowerLogic] Remote Play Active. Force Performance.")
			if err := pm.SetPerformance("Remote Play Active"); err != nil {
				log.Printf("Failed to set performance: %v\n", err)
			}
		} else if isIdle {
			// Idle takes precedence over Game Running (unless Remote Play is active)
			log.Println("[PowerLogic] System Idle. Force Power Saver.")
			if err := pm.SetPowersave("System Idle"); err != nil {
				log.Printf("Failed to set powersave: %v\n", err)
			}
		} else if isGameRunning {
			// Not Idle, Game Running -> Performance
			log.Println("[PowerLogic] Active & Game Running. Force Performance.")
			if err := pm.SetPerformance("Game Active"); err != nil {
				log.Printf("Failed to set performance: %v\n", err)
			}
		} else {
			// Not Idle, No Game -> Auto Detect
			log.Println("[PowerLogic] Active & No Game. Auto-Detect.")
			if err := pm.AutoDetect(); err != nil {
				log.Printf("Failed to auto-detect: %v\n", err)
			}
		}
	}

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
		// This requires Raw Sockets (CAP_NET_RAW or Root)
		if err := rpDet.Start(rpCtx,
			func() {
				isRemotePlay = true
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

	// Create a context for the detector
	detCtx, detCancel := context.WithCancel(context.Background())
	defer detCancel()

	go steamDet.Start(detCtx,
		func(pid int) {
			// On Game Start
			log.Printf("Steam Game Started (PID: %d).\n", pid)
			isGameRunning = true
			if !isIdle {
				if err := pm.SetPerformance(fmt.Sprintf("Steam Game (PID %d)", pid)); err != nil {
					log.Printf("Failed to set performance mode: %v\n", err)
				}
			} else {
				log.Println("Steam Game Started but System is Idle. Keeping Power Saver.")
			}
		},
		func() {
			// On Game Stop
			log.Println("Steam Game Stopped.")
			isGameRunning = false
			// If idle, we stay in powersave (which is consistent).
			// If active, we revert to auto-detect.
			if !isIdle {
				if err := pm.AutoDetect(); err != nil {
					log.Printf("Failed to revert to auto-detect: %v\n", err)
				}
			}
		},
	)

	// Start API Server
	jwtSecret := strings.TrimSpace(CLI.Serve.JWTSecret)
	apiServer := api.NewServer(pm, jwtSecret)

	// Apply middleware if secret is set
	if jwtSecret != "" {
		log.Println("JWT Authentication enabled")
		http.HandleFunc("/mode", apiServer.AuthMiddleware(apiServer.HandleMode))
		http.HandleFunc("/status", apiServer.AuthMiddleware(apiServer.HandleStatus))
	} else {
		log.Println("Warning: JWT Authentication disabled (no secret provided)")
		http.HandleFunc("/mode", apiServer.HandleMode)
		http.HandleFunc("/status", apiServer.HandleStatus)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		addr := fmt.Sprintf("%s:%d", CLI.Serve.Address, CLI.Serve.Port)
		log.Printf("Listening on %s...\n", addr)
		if err := http.ListenAndServe(addr, nil); err != nil {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	<-stop
	log.Println("Shutting down...")
}
