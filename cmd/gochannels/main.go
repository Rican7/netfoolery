package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/Rican7/netfoolery/internal/analytics"
	"github.com/Rican7/netfoolery/internal/run"
)

const (
	appName    = "gochannels"
	appSummary = "Test/Benchmark raw Go channel communication rates"
)

func main() {
	flagSet := flag.NewFlagSet(appName, flag.ExitOnError)

	app := run.NewSingleCommandApp(
		appName,
		appSummary,
		loop,
		flagSet,
		os.Stdout,
		os.Stderr,
	)

	exitCode := app.Run(context.Background(), os.Args[1:])

	os.Exit(exitCode)
}

func loop(ctx context.Context, arguments []string, out io.Writer) error {
	fmt.Fprintln(out, "Starting to loop...")
	defer fmt.Fprintln(out, "\nStopping...")

	dataChan := make(chan *struct{})
	analytics := analytics.New()
	go func() {
		for {
			select {
			case <-ctx.Done():
				close(dataChan)
				return
			default:
				dataChan <- nil
			}
		}
	}()

	for range dataChan {
		total, rate := analytics.IncrForTime(time.Now().Unix())
		fmt.Fprintf(out, "\rLooped. Total: %d. Rate: %d/second", total, rate)
	}

	return nil
}
