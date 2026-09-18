package agentservice

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"
)

// probeSOCKSTCP is deliberately limited to the local, anonymous IPv4 Hans
// proxy. It never transfers a credential and has no unbounded I/O goroutines.
func probeSOCKSTCP(parent context.Context, proxyAddress, targetAddress string) error {
	proxyHost, _, err := net.SplitHostPort(proxyAddress)
	if err != nil || !net.ParseIP(proxyHost).IsLoopback() {
		return fmt.Errorf("Hans readiness proxy must be loopback")
	}
	host, portText, err := net.SplitHostPort(targetAddress)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host).To4()
	port, err := strconv.Atoi(portText)
	if ip == nil || err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("invalid Hans readiness destination")
	}
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", proxyAddress)
	if err != nil {
		return err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	_ = conn.SetDeadline(deadline)
	if _, err := conn.Write([]byte{5, 1, 0}); err != nil {
		return err
	}
	var greeting [2]byte
	if _, err := io.ReadFull(conn, greeting[:]); err != nil {
		return err
	}
	if greeting != [2]byte{5, 0} {
		return fmt.Errorf("invalid Hans SOCKS greeting")
	}
	request := []byte{5, 1, 0, 1, ip[0], ip[1], ip[2], ip[3], 0, 0}
	binary.BigEndian.PutUint16(request[8:], uint16(port))
	if _, err := conn.Write(request); err != nil {
		return err
	}
	var response [10]byte
	if _, err := io.ReadFull(conn, response[:]); err != nil {
		return err
	}
	if response[0] != 5 || response[1] != 0 || response[3] != 1 {
		return fmt.Errorf("Hans SOCKS CONNECT failed with status %d", response[1])
	}
	return nil
}
