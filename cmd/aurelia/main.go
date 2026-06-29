package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/igormaneschy/aurelia/internal/observability"
	"github.com/igormaneschy/aurelia/internal/onboarding"
	"github.com/igormaneschy/aurelia/internal/version"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "onboard":
			if err := onboarding.RunOnboard(os.Stdin, os.Stdout); err != nil {
				log.Fatalf("Failed to run onboarding: %v", err)
			}
			return
		case "cron":
			if err := runCronCLI(os.Args[2:]); err != nil {
				log.Fatalf("Cron command failed: %v", err)
			}
			return
		case "telegram":
			if err := runTelegramCLI(os.Args[2:]); err != nil {
				log.Fatalf("Telegram command failed: %v", err)
			}
			return
		case "migrate-multi-user":
			if err := runMigrateMultiUser(os.Args[2:]); err != nil {
				log.Fatalf("Migration failed: %v", err)
			}
			return
		case "ensure-context-memory":
			if err := runEnsureContextMemoryLayout(os.Args[2:]); err != nil {
				log.Fatalf("Context-memory migration failed: %v", err)
			}
			return
		case "migrate-cwd-overlay":
			if err := runMigrateCwdOverlayPaths(os.Args[2:]); err != nil {
				log.Fatalf("CWD overlay migration failed: %v", err)
			}
			return
		case "version":
			fmt.Println(version.BuildInfo())
			return
		case "debug":
			if err := debugCommand(os.Args[2:]); err != nil {
				log.Fatalf("Debug command failed: %v", err)
			}
			return
		default:
			log.Fatalf("Unknown command: %s", os.Args[1])
		}
	}

	// Ensure only one daemon instance runs at a time.
	unlock := ensureSingleInstance()

	app, err := bootstrapApp()
	if err != nil {
		log.Fatalf("Failed to bootstrap Aurelia: %v", err)
	}

	// Guardrail: require onboarding before starting daemon
	if !app.config.Onboarded() {
		log.Println("Aurelia is not configured yet.")
		log.Println("Run the onboarding wizard first:")
		log.Println("")
		log.Println("    go run ./cmd/aurelia/ onboard")
		log.Println("")
		log.Println("Then start the daemon:")
		log.Println("")
		log.Println("    go run ./cmd/aurelia/")
		os.Exit(1)
	}

	// Set up structured logging from config.
	observability.InitLogger(observability.LoggerConfig{
		Level:  app.config.LogLevel,
		Format: app.config.LogFormat,
	})
	slog.Info("starting aurelia", "version", version.BuildInfo())

	defer app.close()
	defer unlock()

	app.start()
	waitForShutdownSignal()

	log.Println("Shutting down Aurelia...")
	app.shutdown(context.Background())
}

func waitForShutdownSignal() {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
}
