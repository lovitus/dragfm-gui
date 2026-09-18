package rsyncbridge

import (
	"bufio"
	"encoding/json"
	"io"
	"net"
	"testing"
	"time"
)

func TestChildReturnsAtRemoteEOFWhileRsyncKeepsStdinOpen(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	parentDone := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			parentDone <- err
			return
		}
		defer conn.Close()
		line, err := bufio.NewReader(conn).ReadBytes('\n')
		var request request
		if err == nil {
			err = json.Unmarshal(line, &request)
		}
		if err == nil {
			_, err = conn.Write([]byte{1})
		}
		parentDone <- err
	}()
	stdin, producer := io.Pipe()
	defer stdin.Close()
	defer producer.Close() // Deliberately kept open while ChildMain runs.
	done := make(chan int, 1)
	go func() {
		_, code := ChildMain([]string{ChildFlag, listener.Addr().String(), "fixture-token"}, stdin, io.Discard, io.Discard)
		done <- code
	}()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("child exit %d", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("rsh child waited for rsync's still-open stdin after remote EOF")
	}
	if err := <-parentDone; err != nil {
		t.Fatal(err)
	}
}
