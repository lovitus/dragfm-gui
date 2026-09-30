package agentservice

import "github.com/flyssh/flyssh/pkg/connector"

// Embedded transfer helpers target Linux. Do not infer Unix key ownership
// from a Windows controller's environment if this package is built there.
func initiatingAccountIdentity() ([]connector.PrivateKey, bool) { return nil, false }
