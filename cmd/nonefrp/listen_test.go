package main

import (
	"net"
	"testing"
)

func TestListenLocalFallsBackWhenThePortIsBusy(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = busy.Close() }()

	listener, address, err := listenLocal(busy.Addr().String())
	if err != nil {
		t.Fatalf("listenLocal: %v", err)
	}
	defer func() { _ = listener.Close() }()

	if address == busy.Addr().String() {
		t.Fatal("the busy address was reused")
	}
	if listener.Addr().String() != address {
		t.Fatalf("reported %q but bound %q", address, listener.Addr().String())
	}
}

func TestListenLocalUsesTheAskedPortWhenFree(t *testing.T) {
	listener, address, err := listenLocal("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listenLocal: %v", err)
	}
	defer func() { _ = listener.Close() }()

	if address != "127.0.0.1:0" {
		t.Fatalf("address = %q", address)
	}
}
