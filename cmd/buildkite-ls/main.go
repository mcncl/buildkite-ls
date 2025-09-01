package main

import (
	"context"
	"io"
	"os"

	"go.lsp.dev/jsonrpc2"

	"github.com/mcncl/buildkite-ls/internal/lsp"
)

type stdio struct{}

func (stdio) Read(p []byte) (n int, err error)  { return os.Stdin.Read(p) }
func (stdio) Write(p []byte) (n int, err error) { return os.Stdout.Write(p) }
func (stdio) Close() error                      { return nil }

func main() {
	server := lsp.NewServer()
	
	var rw io.ReadWriteCloser = stdio{}
	stream := jsonrpc2.NewStream(rw)
	conn := jsonrpc2.NewConn(stream)
	
	go conn.Go(context.Background(), server.Handler())
	<-conn.Done()
}