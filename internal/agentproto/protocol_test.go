package agentproto

import (
	"net"
	"testing"
)

func TestEncryptedRoundTrip(t *testing.T) {
	clientSide, serverSide := net.Pipe()
	defer clientSide.Close()
	defer serverSide.Close()
	serverReady := make(chan *Conn, 1)
	errors := make(chan error, 1)
	go func() {
		connection, err := Server(serverSide, serverSide)
		if err != nil {
			errors <- err
			return
		}
		serverReady <- connection
	}()
	client, err := Client(clientSide, clientSide)
	if err != nil {
		t.Fatal(err)
	}
	var server *Conn
	select {
	case server = <-serverReady:
	case err := <-errors:
		t.Fatal(err)
	}
	written := make(chan error, 1)
	go func() {
		written <- client.Send(Request{Version: 1, ID: "one", Action: "probe", Secret: map[string]string{"password": "not-on-argv"}})
	}()
	var request Request
	if err := server.Receive(&request); err != nil {
		t.Fatal(err)
	}
	if err := <-written; err != nil {
		t.Fatal(err)
	}
	if request.Secret["password"] != "not-on-argv" {
		t.Fatalf("unexpected request: %#v", request)
	}
}
