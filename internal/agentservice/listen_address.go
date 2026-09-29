package agentservice

import (
	"crypto/rand"
	"errors"
	"math/big"
	"net"
	"strconv"
)

// Bind to one concrete assigned interface, never wildcard. Only five random
// high ports are attempted; port zero cannot silently evade the policy.
func listenDataAddress(bind, requestedPort string) (net.Listener, string, error) {
	addresses := localAddresses()
	if bind == "" {
		for _, address := range addresses {
			if ip := net.ParseIP(address); ip != nil && ip.To4() != nil {
				bind = address
				break
			}
		}
		if bind == "" && len(addresses) > 0 {
			bind = addresses[0]
		}
	}
	ip := net.ParseIP(bind)
	if ip == nil || ip.IsUnspecified() || ip.IsMulticast() {
		return nil, "", errors.New("no concrete data-listener interface")
	}
	// The controller reserves five distinct candidates for the entire network
	// attempt, not five bind retries multiplied by five connection retries.
	if requestedPort != "" {
		port, err := strconv.Atoi(requestedPort)
		if err != nil || port < 20000 || port > 60999 {
			return nil, "", errors.New("data port must be in 20000–60999")
		}
		listener, err := net.Listen("tcp", net.JoinHostPort(bind, strconv.Itoa(port)))
		return listener, bind, err
	}
	var last error
	used := make(map[int]bool)
	for attempt := 0; attempt < 5; {
		value, err := rand.Int(rand.Reader, big.NewInt(41000))
		if err != nil {
			return nil, "", err
		}
		port := 20000 + int(value.Int64())
		if used[port] {
			continue
		}
		used[port] = true
		attempt++
		listener, err := net.Listen("tcp", net.JoinHostPort(bind, strconv.Itoa(port)))
		if err == nil {
			return listener, bind, nil
		}
		last = err
	}
	return nil, "", last
}
