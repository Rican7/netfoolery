package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/Rican7/netfoolery/internal/analytics"
	"github.com/Rican7/netfoolery/internal/pkginfo"
	"golang.org/x/sync/errgroup"

	"github.com/Rican7/lieut"
)

const (
	appName    = "udp"
	appSummary = "Test/Benchmark UDP connection rates"
	appUsage   = "<command> [<option>...]"

	defaultCommandUsage = "[<option>...]"
)

type sharedConfig struct {
	host string
	port int

	timeout time.Duration
}

func (c *sharedConfig) Addr() string {
	return net.JoinHostPort(c.host, strconv.Itoa(c.port))
}

var (
	confShared = sharedConfig{
		host: "127.0.0.1",
		port: 58087,

		timeout: 10 * time.Second,
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
	globalFlags.StringVar(&confShared.host, "host", confShared.host, "the UDP host to use")
	globalFlags.IntVar(&confShared.port, "port", confShared.port, "the UDP port to use")
	globalFlags.DurationVar(&confShared.timeout, "timeout", confShared.timeout, "the timeout to use for connections and closures")

	listenerFlags := flag.NewFlagSet("listener", flag.ExitOnError)

	submitFlags := flag.NewFlagSet("submit", flag.ExitOnError)
	submitFlags.IntVar(&confSubmit.numWorkers, "workers", confSubmit.numWorkers, "the number of workers to use (-1 = unlimited)")

	app := lieut.NewMultiCommandApp(appInfo, globalFlags, out, errOut)

	err := errors.Join(
		app.SetCommand(lieut.CommandInfo{Name: "listen", Summary: "Start listening UDP", Usage: defaultCommandUsage}, listen, listenerFlags),
		app.SetCommand(lieut.CommandInfo{Name: "submit", Summary: "Start submitting UDP", Usage: defaultCommandUsage}, submit, submitFlags),
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

func listen(ctx context.Context, arguments []string) error {
	listenConfig := net.ListenConfig{}

	packetConn, err := listenConfig.ListenPacket(ctx, "udp", confShared.Addr())
	if err != nil {
		return err
	}
	defer packetConn.Close()

	udpConn, ok := packetConn.(*net.UDPConn)
	if !ok {
		return fmt.Errorf("unexpected connection type %T", udpConn)
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

	listenAnalytics := analytics.New(false)

	fmt.Fprintf(out, "Starting to listen at host '%s'...\n", udpConn.LocalAddr())

	errChan := make(chan error)
	go func() {
		<-ctx.Done()
		fmt.Fprintln(out, "\nStopping...")
		defer fmt.Fprintln(out, "\nDone.")

		if err := udpConn.SetDeadline(time.Now()); err != nil {
			errChan <- err
		}

		errChan <- udpConn.Close()
	}()

	scanner := bufio.NewScanner(udpConn)
	for scanner.Scan() {
		ticker.Reset(time.Second)

		if err := scanner.Err(); err != nil {
			return err
		}

		msg := scanner.Text()

		if string(msg) != "Ping!" {
			return fmt.Errorf("msg contained unexpected data %q", msg)
		}

		total, rate := listenAnalytics.IncrForTime(time.Now().Unix())
		fmt.Fprintf(out, "\rReceived. Total: %d. Rate: %d/second", total, rate)
	}

	return <-errChan
}

func submit(ctx context.Context, arguments []string) error {
	dialer := net.Dialer{
		Timeout: confShared.timeout,
	}

	conn, err := dialer.DialContext(ctx, "udp", confShared.Addr())
	if err != nil {
		return err
	}

	defer conn.Close()

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
