package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/Rican7/netfoolery/internal/analytics"
	"github.com/Rican7/netfoolery/internal/pkginfo"
	"golang.org/x/sync/errgroup"

	"github.com/Rican7/lieut"
)

const (
	appName    = "tcp"
	appSummary = "Test/Benchmark TCP connection rates"
	appUsage   = "<command> [<option>...]"

	defaultCommandUsage = "[<option>...]"
)

type sharedConfig struct {
	host string
	port int

	timeout       time.Duration
	useKeepAlives bool
}

func (c *sharedConfig) Addr() string {
	return net.JoinHostPort(c.host, strconv.Itoa(c.port))
}

var (
	confShared = sharedConfig{
		host: "127.0.0.1",
		port: 58086,

		timeout:       10 * time.Second,
		useKeepAlives: true,
	}

	confSubmit = struct {
		numWorkers int
	}{
		numWorkers: 3, // 3 seems to run best currently...
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
	globalFlags.StringVar(&confShared.host, "host", confShared.host, "the TCP host to use")
	globalFlags.IntVar(&confShared.port, "port", confShared.port, "the TCP port to use")
	globalFlags.DurationVar(&confShared.timeout, "timeout", confShared.timeout, "the timeout to use for connections and closures")
	globalFlags.BoolVar(&confShared.useKeepAlives, "keep-alives", confShared.useKeepAlives, "enable TCP 'keep-alives'")

	serverFlags := flag.NewFlagSet("server", flag.ExitOnError)

	submitFlags := flag.NewFlagSet("submit", flag.ExitOnError)
	submitFlags.IntVar(&confSubmit.numWorkers, "workers", confSubmit.numWorkers, "the number of workers to use (-1 = unlimited)")

	app := lieut.NewMultiCommandApp(appInfo, globalFlags, out, errOut)

	err := errors.Join(
		app.SetCommand(lieut.CommandInfo{Name: "serve", Summary: "Start serving TCP", Usage: defaultCommandUsage}, serve, serverFlags),
		app.SetCommand(lieut.CommandInfo{Name: "submit", Summary: "Start submitting TCP", Usage: defaultCommandUsage}, submit, submitFlags),
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
	keepAlive := confShared.timeout
	if !confShared.useKeepAlives {
		keepAlive = -1
	}

	listenConfig := net.ListenConfig{
		KeepAlive: keepAlive,
	}

	listener, err := listenConfig.Listen(ctx, "tcp", confShared.Addr())
	if err != nil {
		return err
	}
	defer listener.Close()

	tcpListener, ok := listener.(*net.TCPListener)
	if !ok {
		return fmt.Errorf("unexpected listener type %T", tcpListener)
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

	serveAnalytics := analytics.New(true)

	fmt.Fprintf(out, "Starting to serve at host '%s'...\n", listener.Addr())

	var wg sync.WaitGroup
	errChan := make(chan error)

	go func() {
		select {
		case err := <-errChan:
			tcpListener.Close()
			errChan <- err
		case <-ctx.Done():
			fmt.Fprintln(out, "\nStopping...")
			defer fmt.Fprintln(out, "\nDone.")
			errChan <- tcpListener.Close()
		}
	}()

	for {
		conn, err := tcpListener.AcceptTCP()
		if err != nil {
			if errors.Is(err, net.ErrClosed) && ctx.Err() != nil {
				// Expected error condition, report the context error instead
				err = ctx.Err()
			}

			go func(err error) {
				errChan <- err
			}(err)

			break
		}

		wg.Add(1)
		go func(conn *net.TCPConn) {
			defer conn.Close()
			defer wg.Done()

			ticker.Reset(time.Second)

			msg, err := io.ReadAll(conn)
			if err != nil {
				errChan <- err
			}

			if string(msg) != "Ping!\n" {
				errChan <- fmt.Errorf("msg contained unexpected data %q", msg)
			}

			total, rate := serveAnalytics.IncrForTime(time.Now().Unix())
			fmt.Fprintf(out, "\rReceived. Total: %d. Rate: %d/second", total, rate)
		}(conn)
	}

	wg.Wait()

	return <-errChan
}

func submit(ctx context.Context, arguments []string) error {
	keepAlive := confShared.timeout
	if !confShared.useKeepAlives {
		keepAlive = -1
	}

	dialer := net.Dialer{
		Timeout:   confShared.timeout,
		KeepAlive: keepAlive,
	}

	submitAnalytics := analytics.New(true)

	fmt.Fprintf(out, "Starting to submit to host '%s' with %d workers...\n", confShared.Addr(), confSubmit.numWorkers)

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
			// TODO: Implement TCP connection re-use/pooling
			//
			// Currently, this ends up just exhausting TCP connection resources
			conn, err := dialer.DialContext(ctx, "tcp", confShared.Addr())
			if err != nil {
				return err
			}

			defer conn.Close()

			_, err = fmt.Fprintln(conn, "Ping!")
			if err != nil {
				return err
			}

			total, rate := submitAnalytics.IncrForTime(time.Now().Unix())
			fmt.Fprintf(out, "\rSubmitted. Total: %d. Rate: %d/second", total, rate)

			return nil
		})
	}

	return workers.Wait()
}
