package endpoint

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
)

var fallbackLocalIdentity = sync.OnceValue(func() string {
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		panic(err)
	}
	return "dragfm-controller-process:" + hex.EncodeToString(token[:])
})
