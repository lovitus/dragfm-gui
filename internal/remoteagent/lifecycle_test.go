package remoteagent

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/lovitus/dragfm-gui/internal/agentproto"
)

func stalledHelper(t *testing.T) (*Session, <-chan struct{}) {
	t.Helper()
	client, server := net.Pipe()
	t.Cleanup(func() { _ = client.Close(); _ = server.Close() })
	started := make(chan struct{})
	go func() {
		protocol, err := agentproto.Server(server, server)
		if err != nil {
			return
		}
		var request agentproto.Request
		if protocol.Receive(&request) == nil {
			close(started)
		}
	}()
	protocol, err := agentproto.Client(client, client)
	if err != nil {
		t.Fatal(err)
	}
	return &Session{Protocol: protocol, ssh: client, stdin: client, done: make(chan struct{})}, started
}

func TestCancelledCallDoesNotWaitForBusyProtocolMutex(t *testing.T) {
	helper, started := stalledHelper(t)
	first := make(chan error, 1)
	go func() { _, err := helper.Call("blocked", nil, nil); first <- err }()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := helper.CallContext(ctx, "cancelled", nil, nil); done <- err }()
	for _, result := range []<-chan error{done, first} {
		select {
		case err := <-result:
			if err == nil {
				t.Fatal("stalled request succeeded")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("cancellation waited behind protocol mutex")
		}
	}
}

func TestCloseOfElevatedBusyHelperIsBounded(t *testing.T) {
	helper, started := stalledHelper(t)
	helper.elevated = true
	go helper.Call("blocked", nil, nil)
	<-started
	closed := make(chan struct{})
	go func() { _ = helper.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("privileged cleanup deadlocked")
	}
	if _, err := helper.Call("after-close", nil, nil); err == nil {
		t.Fatal("closed helper accepted call")
	}
}

func TestCallInheritsOwningTransferCancellation(t *testing.T) {
	helper, started := stalledHelper(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	helper.ctx = ctx
	done := make(chan error, 1)
	go func() { _, err := helper.Call("blocked", nil, nil); done <- err }()
	<-started
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled transfer succeeded")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Call ignored transfer cancellation")
	}
}

func TestHelperOutputConcurrentAndBounded(t *testing.T) {
	var output helperOutput
	var writers sync.WaitGroup
	for i := 0; i < 10; i++ {
		writers.Add(1)
		go func() {
			defer writers.Done()
			for j := 0; j < 100; j++ {
				_, _ = output.Write(make([]byte, 1024))
				_ = output.String()
			}
		}()
	}
	writers.Wait()
	if len(output.String()) > 16<<10 {
		t.Fatal("unbounded helper diagnostics")
	}
}
