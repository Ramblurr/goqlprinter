package brotherql

import (
	"io"
	"net"
	"testing"
	"time"
)

func TestTCPProviderSendsOnlyToConfiguredPrinter(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := listener.(*net.TCPListener).SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}

	provider := NewTCPProvider(listener.Addr().String(), "QL-810W")
	printers, err := provider.FindPrinters()
	if err != nil || len(printers) != 1 || printers[0].Model != "QL-810W" {
		t.Fatalf("FindPrinters = %v, %v", printers, err)
	}
	if conn, err := provider.Connect(PrinterInfo{URI: "tcp://127.0.0.1:1"}); err == nil {
		conn.Close()
		t.Fatal("Connect accepted an unconfigured endpoint")
	}

	backend, err := provider.Connect(printers[0])
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	if _, err := backend.Write([]byte("raster job")); err != nil {
		t.Fatal(err)
	}

	server, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	if err := server.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len("raster job"))
	if _, err := io.ReadFull(server, got); err != nil || string(got) != "raster job" {
		t.Fatalf("received %q, %v", got, err)
	}
}
