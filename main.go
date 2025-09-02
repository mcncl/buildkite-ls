package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"go.lsp.dev/jsonrpc2"

	"github.com/mcncl/buildkite-ls/internal/lsp"
)

var (
	// Version information - set during build
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

type stdio struct{}

func (stdio) Read(p []byte) (n int, err error)  { return os.Stdin.Read(p) }
func (stdio) Write(p []byte) (n int, err error) { return os.Stdout.Write(p) }
func (stdio) Close() error                      { return nil }

func main() {
	showVersion := flag.Bool("version", false, "Show version information")
	flag.Parse()

	if *showVersion {
		fmt.Printf("buildkite-ls %s\n", version)
		fmt.Printf("Commit: %s\n", commit)
		fmt.Printf("Built: %s\n", date)
		return
	}

	server := lsp.NewServer()

	var rw io.ReadWriteCloser = stdio{}
	stream := jsonrpc2.NewStream(rw)
	conn := jsonrpc2.NewConn(stream)

	// Set the connection in the server so it can send notifications
	server.SetConnection(conn)

	go conn.Go(context.Background(), server.Handler())
	<-conn.Done()
}
