package agentservice

import (
	"os"
	"time"

	"github.com/flyssh/flyssh/pkg/connector"
	"github.com/lovitus/dragfm-gui/internal/agentroute"
)

// Only remote transfer initiation uses the executing account's identity.
// Discover once per attempt and keep each hop's explicitly selected vault
// credentials independent. Never copy discovered keys into the wire route.
func decodeInitiatorRoute(payload string) (connector.Route, error) {
	route, err := agentroute.Decode(payload, os.Geteuid())
	if err != nil {
		return connector.Route{}, err
	}
	var keys []connector.PrivateKey
	var agentAllowed, discovered bool
	for i := range route.Hops {
		credentials := &route.Hops[i].Credentials
		if !credentials.UseAgent {
			continue
		}
		if !discovered {
			keys, agentAllowed = initiatingAccountIdentity()
			discovered = true
		}
		credentials.UseAgent = agentAllowed
		credentials.AgentTimeout = 5 * time.Second
		credentials.PrivateKeys = append(append([]connector.PrivateKey(nil), keys...), credentials.PrivateKeys...)
	}
	return route, nil
}
