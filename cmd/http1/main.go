package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/Rican7/netfoolery/internal/analytics"
	"github.com/Rican7/netfoolery/internal/run"
)

const (
	appName    = "http1"
	appSummary = "Test/Benchmark HTTP/1.x connection rates"
)

type config struct {
	port int
}

func (c *config) Addr() string {
	return fmt.Sprintf(":%d", c.port)
}

func (c *config) URLString() string {
	return fmt.Sprintf("http://localhost:%d", c.port)
}

var (
	conf config
)

func main() {
	flagSet := flag.NewFlagSet(appName, flag.ExitOnError)
	flagSet.IntVar(&conf.port, "port", 58085, "the HTTP port to use")

	app := run.NewMultiCommandApp(appName, appSummary, flagSet, os.Stdout, os.Stderr)

	err := errors.Join(
		app.SetCommand("serve", "Start serving HTTP/1.x", serve, nil),
		app.SetCommand("submit", "Start submitting HTTP/1.x", submit, nil),
	)
	if err != nil {
		panic(err)
	}

	exitCode := app.Run(context.Background(), os.Args[1:])

	os.Exit(exitCode)
}

func serve(ctx context.Context, out io.Writer) error {
	server := http.Server{
		Addr: conf.Addr(),

		// Disable HTTP/2 by setting this to a non-nil, empty map
		TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
	}

	errChan := make(chan error)
	go func() {
		<-ctx.Done()
		defer fmt.Fprintln(out, "\nShutting down....")
		errChan <- server.Shutdown(ctx)
	}()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	go func() {
		for {
			select {
			case <-ticker.C:
				fmt.Fprintf(out, "\r\033[2KWaiting...")
			case <-ctx.Done():
				return
			}
		}
	}()

	serveAnalytics := analytics.New()
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ticker.Reset(time.Second)
		total, rate := serveAnalytics.IncrForTime(time.Now().Unix())
		fmt.Fprintf(out, "\rReceived. Total: %d. Rate: %d/second", total, rate)
	})

	fmt.Fprintf(out, "Starting to serve on port %d...\n", conf.port)

	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}

	return <-errChan
}

func submit(ctx context.Context, out io.Writer) error {
	client := http.Client{}
	submitAnalytics := analytics.New()

	fmt.Fprintf(out, "Starting to submit on port %d...\n", conf.port)
	defer fmt.Fprintln(out, "\nStopping...")

	var err error
	for err == nil {
		var req *http.Request
		var resp *http.Response

		req, err = http.NewRequestWithContext(ctx, http.MethodPost, conf.URLString(), nil)
		if err != nil {
			return err
		}

		resp, err = client.Do(req)
		if err != nil {
			return err
		}

		defer resp.Body.Close()

		total, rate := submitAnalytics.IncrForTime(time.Now().Unix())
		fmt.Fprintf(out, "\rSubmitted. Total: %d. Rate: %d/second", total, rate)
	}

	return err
}
