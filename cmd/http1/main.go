package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
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

var conf config

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

	var serveCounter atomic.Int64

	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := serveCounter.Add(1)
		fmt.Printf("\rgot it: %d", count)
	})

	return server.ListenAndServe()
}

func submit(ctx context.Context) error {
	client := http.Client{}

	var submitCounter atomic.Int64
	var err error
	for err == nil {
		count := submitCounter.Add(1)
		fmt.Printf("\rsubmitting: %d", count)

		var resp *http.Response
		resp, err = client.Post(conf.URLString(), "application/netfoolery", nil)

		if err == nil {
			defer resp.Body.Close()
		}
	}

	return err
}
