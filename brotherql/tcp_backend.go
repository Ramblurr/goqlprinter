package brotherql

import (
	"fmt"
	"net"
	"time"
)

const tcpIOTimeout = 3 * time.Second

// TCPProvider connects to one configured raw TCP printer.
type TCPProvider struct {
	address string
	model   string
}

func NewTCPProvider(address, model string) *TCPProvider {
	return &TCPProvider{address: address, model: model}
}

func (p *TCPProvider) FindPrinters() ([]PrinterInfo, error) {
	if _, err := GetModel(p.model); err != nil {
		return nil, err
	}
	return []PrinterInfo{{
		Name:    p.model,
		Model:   p.model,
		URI:     "tcp://" + p.address,
		Backend: "tcp",
	}}, nil
}

func (p *TCPProvider) Connect(printer PrinterInfo) (Backend, error) {
	if printer.URI != "tcp://"+p.address {
		return nil, fmt.Errorf("TCP printer %q is not configured", printer.URI)
	}
	conn, err := net.DialTimeout("tcp", p.address, tcpIOTimeout)
	if err != nil {
		return nil, err
	}
	host, _, _ := net.SplitHostPort(p.address)
	return &tcpBackend{
		Conn:   conn,
		ippURL: "http://" + net.JoinHostPort(host, "631") + "/ipp/print",
	}, nil
}

func (p *TCPProvider) SupportsStatus() bool { return true }

type tcpBackend struct {
	net.Conn
	ippURL string
}

func (b *tcpBackend) IPPStatus() (PrinterStatus, error) {
	return queryIPPStatus(b.ippURL)
}

// Read fails at once because port 9100 never sends data back.
func (b *tcpBackend) Read([]byte) (int, error) {
	return 0, ErrStatusNotSupported
}

func (b *tcpBackend) Write(data []byte) (int, error) {
	if err := b.SetWriteDeadline(time.Now().Add(tcpIOTimeout)); err != nil {
		return 0, err
	}
	return b.Conn.Write(data)
}
