//go:build testing

package main

import (
	"net"
	"testing"
)

func TestMockListenerIsLoopbackOnly(t *testing.T) {
	listener, err := listenLocal(0)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if address, ok := listener.Addr().(*net.TCPAddr); !ok || !address.IP.IsLoopback() {
		t.Fatalf("mock listener exposed outside loopback: %v", listener.Addr())
	}
}
