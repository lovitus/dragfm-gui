//go:build linux && integration

package agentservice_test

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lovitus/dragfm-gui/internal/agentproto"
	"github.com/lovitus/dragfm-gui/internal/agentservice"
	"github.com/lovitus/dragfm-gui/internal/assets"
)

func TestOfficialHansReleaseThroughAgentService(t *testing.T) {
	root := t.TempDir()
	binary, _, err := assets.LinuxHans("amd64")
	if err != nil {
		t.Fatal(err)
	}
	binaryPath := filepath.Join(root, "hans")
	if err := os.WriteFile(binaryPath, binary, 0700); err != nil {
		t.Fatal(err)
	}

	echo, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	go func() {
		connection, acceptErr := echo.Accept()
		if acceptErr != nil {
			return
		}
		defer connection.Close()
		_, _ = io.Copy(connection, connection)
	}()

	server := agentservice.New()
	serverJob := "hans-integration-server"
	serverResponse := server.Handle(agentproto.Request{
		Version: agentproto.ProtocolVersion,
		ID:      "server-start",
		Action:  "hans-server-start",
		Options: map[string]string{
			"binary": binaryPath, "identity": filepath.Join(root, "server.key"),
			"job": serverJob, "network": "10.253.77.0", "lease": filepath.Join(root, "leases"),
		},
		Secret: map[string]string{"passphrase": "dragfm-hans-integration-secret"},
	})
	if !serverResponse.OK || serverResponse.Values["fingerprint"] == "" {
		t.Fatalf("start server: %#v", serverResponse)
	}
	defer stopHans(server, serverJob)

	proxyPort := reserveTCPPort(t)
	proxyAddress := fmt.Sprintf("127.0.0.1:%d", proxyPort)
	client := agentservice.New()
	clientJob := "hans-integration-client"
	clientResponse := client.Handle(agentproto.Request{
		Version: agentproto.ProtocolVersion,
		ID:      "client-start",
		Action:  "hans-client-start",
		Options: map[string]string{
			"binary": binaryPath, "identity": filepath.Join(root, "client.key"),
			"job": clientJob, "server": "127.0.0.1", "socks": proxyAddress,
		},
		Secret: map[string]string{
			"passphrase":  "dragfm-hans-integration-secret",
			"fingerprint": serverResponse.Values["fingerprint"],
		},
	})
	if !clientResponse.OK {
		t.Fatalf("start client: %#v", clientResponse)
	}
	defer stopHans(client, clientJob)

	destinationPort := echo.Addr().(*net.TCPAddr).Port
	deadline := time.Now().Add(20 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		if lastErr = roundTripSOCKS(proxyAddress, net.ParseIP("10.253.77.1"), destinationPort); lastErr == nil {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	for _, child := range []struct {
		service *agentservice.Service
		job     string
	}{{server, serverJob}, {client, clientJob}} {
		reply := child.service.Handle(agentproto.Request{Version: agentproto.ProtocolVersion, ID: "diagnose", Action: "process-diagnostics", Options: map[string]string{"job": child.job}})
		t.Logf("%s: %s", child.job, reply.Values["output"])
	}
	t.Fatalf("official Hans userspace tunnel did not carry TCP: %v", lastErr)
}

func stopHans(service *agentservice.Service, job string) {
	service.Handle(agentproto.Request{Version: agentproto.ProtocolVersion, ID: "stop-" + job, Action: "process-stop", Options: map[string]string{"job": job}})
}

func reserveTCPPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	return port
}

func roundTripSOCKS(proxyAddress string, destination net.IP, port int) error {
	connection, err := net.DialTimeout("tcp", proxyAddress, time.Second)
	if err != nil {
		return err
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := connection.Write([]byte{5, 1, 0}); err != nil {
		return err
	}
	var greeting [2]byte
	if _, err := io.ReadFull(connection, greeting[:]); err != nil || greeting != [2]byte{5, 0} {
		return fmt.Errorf("SOCKS greeting %v: %w", greeting, err)
	}
	ipv4 := destination.To4()
	if ipv4 == nil {
		return fmt.Errorf("destination is not IPv4: %s", destination)
	}
	request := []byte{5, 1, 0, 1, ipv4[0], ipv4[1], ipv4[2], ipv4[3], 0, 0}
	binary.BigEndian.PutUint16(request[8:], uint16(port))
	if _, err := connection.Write(request); err != nil {
		return err
	}
	var response [10]byte
	if _, err := io.ReadFull(connection, response[:]); err != nil {
		return err
	}
	if response[0] != 5 || response[1] != 0 || response[3] != 1 {
		return fmt.Errorf("SOCKS connect response %v", response)
	}
	payload := []byte("dragfm-hans-tunnel-ok")
	if _, err := connection.Write(payload); err != nil {
		return err
	}
	received := make([]byte, len(payload))
	if _, err := io.ReadFull(connection, received); err != nil {
		return err
	}
	if string(received) != string(payload) {
		return fmt.Errorf("echo mismatch: %q", received)
	}
	return nil
}
