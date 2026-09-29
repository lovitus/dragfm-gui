package remoteagent

import (
	"context"
	"sync"

	"github.com/lovitus/dragfm-gui/internal/endpoint"
)

// Startup may block before a session exists, while sending credentials, or
// while reading a protocol hello. CHANNEL_CLOSE alone cannot interrupt these
// reads. Callers supply a task-owned Fork, never the browser's transport.
// Stopping the watcher joins a callback that has already begun, so a deadline
// cannot race a successfully returned view and close it later.
func watchStartup(ctx context.Context, remote *endpoint.Remote) func() {
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		_ = remote.Close()
		close(done)
	})
	var once sync.Once
	return func() {
		once.Do(func() {
			if !stop() {
				<-done
			}
		})
	}
}
