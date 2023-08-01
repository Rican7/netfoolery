package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"time"
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

type timeCountPair struct {
	ts    int64
	count uint
}

type analytics struct {
	totalCount uint
	timeCount  []timeCountPair

	mutex sync.RWMutex
}

func newAnalytics() *analytics {
	return &analytics{
		timeCount: make([]timeCountPair, 2),
	}
}

func (a *analytics) TotalCount() uint {
	a.mutex.RLock()
	defer a.mutex.RUnlock()

	return a.totalCount
}

func (a *analytics) CountPerSecond() uint {
	a.mutex.RLock()
	defer a.mutex.RUnlock()

	// Return the oldest data, as it's "complete"
	return a.timeCount[0].count
}

func (a *analytics) IncrForTime(unixTime int64) (uint, uint) {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	a.totalCount++
	a.setTimeCount(unixTime)

	return a.totalCount, a.timeCount[0].count
}

func (a *analytics) setTimeCount(unixTime int64) {
	latestIndex := len(a.timeCount) - 1

	if a.timeCount[latestIndex].ts != unixTime {
		// Remove the oldest, add a latest
		a.timeCount = a.timeCount[1:]
		a.timeCount = append(a.timeCount, timeCountPair{ts: unixTime})
	}

	a.timeCount[latestIndex].count++
}

var (
	conf config
)

func init() {
	flag.IntVar(&conf.port, "port", 58085, "the HTTP port to use")

	flag.Parse()
}

func main() {
	os.Exit(run())
}

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	errChan := make(chan error)

	switch flag.Arg(0) {
	case "serve":
		fmt.Println("Starting to serve...")
		go func() {
			errChan <- serve(ctx)
		}()
	case "submit":
		fmt.Println("Starting to submit...")
		go func() {
			errChan <- submit(ctx)
		}()
	default:
		return printErr(fmt.Errorf("expected 'serve' or 'submit'"))
	}

	select {
	case err := <-errChan:
		returnCode := 0

		if err != nil {
			printErr(err)
			returnCode = 1
		}

		stop()

		return returnCode
	case <-ctx.Done():
		fmt.Printf("\n\nDone.\n")
		stop()
	}

	return 0
}

func printErr(err error) int {
	fmt.Fprintf(os.Stderr, "\n\nError: %v\n", err)

	time.Sleep(1 * time.Second)

	return 1
}

func serve(ctx context.Context) error {
	server := http.Server{
		Addr: conf.Addr(),

		// Disable HTTP/2 by setting this to a non-nil, empty map
		TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
	}

	serveAnalytics := newAnalytics()
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		total, rate := serveAnalytics.IncrForTime(time.Now().Unix())
		fmt.Printf("\rReceiving. Total: %d. Rate: %d/second", total, rate)
	})

	return server.ListenAndServe()
}

func submit(ctx context.Context) error {
	client := http.Client{}

	submitAnalytics := newAnalytics()
	var err error
	for err == nil {
		total, rate := submitAnalytics.IncrForTime(time.Now().Unix())
		fmt.Printf("\rSubmitting. Total: %d. Rate: %d/second", total, rate)

		var resp *http.Response
		resp, err = client.Post(conf.URLString(), "application/netfoolery", nil)

		if err == nil {
			defer resp.Body.Close()
		}
	}

	return err
}
