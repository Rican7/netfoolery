package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/Rican7/netfoolery/internal/analytics"
	"github.com/Rican7/netfoolery/internal/run"
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
	flagSet := flag.CommandLine
	flagSet.IntVar(&conf.port, "port", 58085, "the HTTP port to use")

	app := run.NewApp(os.Args[0], os.Stdout, os.Stderr)
	app.SetCommand("serve", serve, flagSet)
	app.SetCommand("submit", submit, flagSet)

	ctx := context.Background()

	exitCode := app.Run(ctx, os.Args[1:])

	os.Exit(exitCode)
}

func serve(ctx context.Context, out io.Writer) error {
	server := http.Server{
		Addr: conf.Addr(),

		// Disable HTTP/2 by setting this to a non-nil, empty map
		TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
	}

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

	return server.ListenAndServe()
}

func submit(ctx context.Context, out io.Writer) error {
	client := http.Client{}

	submitAnalytics := analytics.New()
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
