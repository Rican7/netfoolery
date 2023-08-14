package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/Rican7/netfoolery/internal/analytics"
	"github.com/Rican7/netfoolery/internal/pkginfo"

	"github.com/Rican7/lieut"
)

const (
	appName    = "gochannels"
	appSummary = "Test/Benchmark raw Go channel communication rates"
	appUsage   = "[<option>...]"
)

var (
	out    = os.Stdout
	errOut = os.Stderr
)

func main() {
	appInfo := lieut.AppInfo{
		Name:    appName,
		Summary: appSummary,
		Usage:   appUsage,
		Version: pkginfo.Version,
	}

	flagSet := flag.NewFlagSet(appName, flag.ExitOnError)

	app := lieut.NewSingleCommandApp(
		appInfo,
		loop,
		flagSet,
		out,
		errOut,
	)

	exitCode := app.Run(context.Background(), os.Args[1:])

	os.Exit(exitCode)
}

func loop(ctx context.Context, arguments []string) error {
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
