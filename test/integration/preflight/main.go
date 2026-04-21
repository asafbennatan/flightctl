// Command preflight starts or stops testcontainers used by integration tests (Postgres, Redis, Alertmanager).
//
// Usage:
//
//	go run ./test/integration/preflight start
//	go run ./test/integration/preflight stop
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/flightctl/flightctl/test/integration/integrationstack"
	"github.com/sirupsen/logrus"
)

func main() {
	logrus.SetLevel(logrus.InfoLevel)
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: preflight start|stop")
		os.Exit(2)
	}
	ctx := context.Background()
	switch os.Args[1] {
	case "start":
		if err := integrationstack.EnsureRunning(ctx); err != nil {
			logrus.Fatalf("preflight start: %v", err)
		}
	case "stop":
		if err := integrationstack.Stop(ctx); err != nil {
			logrus.Fatalf("preflight stop: %v", err)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n", os.Args[1])
		os.Exit(2)
	}
}
