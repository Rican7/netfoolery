package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"time"

	"github.com/Rican7/netfoolery/internal/analytics"
	"github.com/Rican7/netfoolery/internal/pkginfo"

	"github.com/Rican7/lieut"
	"golang.org/x/sync/errgroup"
)

const (
	appName    = "http1"
	appSummary = "Test/Benchmark HTTP/1.x connection rates"
	appUsage   = "<command> [<option>...]"

	defaultCommandUsage = "[<option>...]"
)

type sharedConfig struct {
	host string
	port int
}

func (c *sharedConfig) Addr() string {
	return net.JoinHostPort(c.host, strconv.Itoa(c.port))
}

func (c *sharedConfig) URLString() string {
	return fmt.Sprintf("http://%s", c.Addr())
}

var (
	confShared = sharedConfig{
		host: "localhost",
		port: 58085,
	}

	confServer = struct {
		useHTTPKeepAlives bool
	}{
		useHTTPKeepAlives: true,
	}

	confSubmit = struct {
		numWorkers int
	}{
		numWorkers: runtime.NumCPU(),
	}

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

	globalFlags := flag.NewFlagSet(appName, flag.ExitOnError)
	globalFlags.StringVar(&confShared.host, "host", confShared.host, "the HTTP host to use")
	globalFlags.IntVar(&confShared.port, "port", confShared.port, "the HTTP port to use")

	serverFlags := flag.NewFlagSet("server", flag.ExitOnError)
	serverFlags.BoolVar(&confServer.useHTTPKeepAlives, "http-keep-alives", confServer.useHTTPKeepAlives, "enable HTTP 'keep-alives'")

	submitFlags := flag.NewFlagSet("submit", flag.ExitOnError)
	submitFlags.IntVar(&confSubmit.numWorkers, "workers", confSubmit.numWorkers, "the number of workers to use (-1 = unlimited)")

	app := lieut.NewMultiCommandApp(appInfo, globalFlags, out, errOut)

	err := errors.Join(
		app.SetCommand(lieut.CommandInfo{Name: "serve", Summary: "Start serving HTTP/1.x", Usage: defaultCommandUsage}, serve, serverFlags),
		app.SetCommand(lieut.CommandInfo{Name: "submit", Summary: "Start submitting HTTP/1.x", Usage: defaultCommandUsage}, submit, submitFlags),
	)
	if err != nil {
		panic(err)
	}

	app.OnInit(validateConfig)

	exitCode := app.Run(context.Background(), os.Args[1:])

	os.Exit(exitCode)
}

func validateConfig() error {
	if confSubmit.numWorkers == 0 {
		return fmt.Errorf("invalid worker count '%d'", confSubmit.numWorkers)
	}

	return nil
}

func serve(ctx context.Context, arguments []string) error {
	server := http.Server{
		Addr: confShared.Addr(),

		// Disable HTTP/2 by setting this to a non-nil, empty map
		TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
	}
	server.SetKeepAlivesEnabled(confServer.useHTTPKeepAlives)

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

	serveAnalytics := analytics.New(true)
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ticker.Reset(time.Second)
		total, rate := serveAnalytics.IncrForTime(time.Now().Unix())
		fmt.Fprintf(out, "\rReceived. Total: %d. Rate: %d/second", total, rate)
	})

	fmt.Fprintf(out, "Starting to serve at host '%s'...\n", server.Addr)

	errChan := make(chan error)
	go func() {
		// Wait for multiple potential signals
		select {
		case err := <-errChan:
			// The error came from starting the server.
			// Put the error back and bail.
			errChan <- err
			return
		case <-ctx.Done():
			// Context was canceled...
		}

		timeout := 10 * time.Second
		fmt.Fprintf(out, "\nShutting down (timeout %s)...\n", timeout)
		defer fmt.Fprintln(out, "\nDone.")

		// Create a new timeout context to give the server a chance to shutdown
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()

		errChan <- server.Shutdown(ctx)
	}()

	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		errChan <- err
	}

	return <-errChan
}

func submit(ctx context.Context, arguments []string) error {
	transport := &http.Transport{
		Proxy:             http.ProxyFromEnvironment,
		ForceAttemptHTTP2: false,

		// Set the number of max connections AND idle connections per-host to
		// the number of workers, so that we don't end up breaching this maximum
		// and causing connections to be constantly ended and connected, rather
		// than reused from the connection pool.
		//
		// See:
		//  - https://stackoverflow.com/a/37813844
		//  - https://stackoverflow.com/a/39834253
		MaxConnsPerHost:     confSubmit.numWorkers,
		MaxIdleConnsPerHost: confSubmit.numWorkers,
		MaxIdleConns:        confSubmit.numWorkers * 100,

		IdleConnTimeout: 10 * time.Second,
	}

	client := http.Client{
		Transport: transport,
	}
	submitAnalytics := analytics.New(true)

	fmt.Fprintf(out, "Starting to submit to URL '%s' with %d workers...\n", confShared.URLString(), confSubmit.numWorkers)

	workers, ctx := errgroup.WithContext(ctx)
	workers.SetLimit(confSubmit.numWorkers)

	for confSubmit.numWorkers != 0 {
		select {
		case <-ctx.Done():
			fmt.Fprintln(out, "\nStopping...")
			defer fmt.Fprintln(out, "\nDone.")
			return workers.Wait()
		default:
			// Continue as normal
		}

		workers.Go(func() error {
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, confShared.URLString(), nil)
			if err != nil {
				return err
			}

			resp, err := client.Do(req)
			if err != nil {
				return err
			}

			defer resp.Body.Close()

			total, rate := submitAnalytics.IncrForTime(time.Now().Unix())
			fmt.Fprintf(out, "\rSubmitted. Total: %d. Rate: %d/second", total, rate)

			return nil
		})
	}

	return workers.Wait()
}
