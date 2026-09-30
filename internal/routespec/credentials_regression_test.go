package routespec

import (
	"strings"
	"testing"
)

func TestSOCKSShorthandPreservesLiteralCredentialBytes(t *testing.T) {
	for _, password := range []string{"p%40ss", "p#ss", "p@ss", "p:ss", "p%#@:\"ss"} {
		t.Run(password, func(t *testing.T) {
			proxy, err := ParseSOCKS("user:" + password + "@[::1]:1080")
			if err != nil {
				t.Fatal(err)
			}
			if proxy.Username != "user" || proxy.Password != password || proxy.Address != "[::1]:1080" {
				t.Fatal("SOCKS shorthand changed credential bytes")
			}
		})
	}
}

func TestUnsavedRouteParseErrorsNeverEchoCredentials(t *testing.T) {
	const secret = "unsaved-fixture-secret"
	for _, input := range []string{"user:" + secret, "-" + secret} {
		_, err := ParseSSH(input, nil)
		if err == nil || strings.Contains(err.Error(), secret) {
			t.Fatal("SSH parse error disclosed unsaved credentials or accepted malformed input")
		}
	}
	for _, input := range []string{"socks5://user:" + secret + "%ZZ@127.0.0.1:1080", "user:" + secret + "@[broken:1080", "user:" + secret + "@127.0.0.1:noport"} {
		_, err := ParseSOCKS(input)
		if err == nil || strings.Contains(err.Error(), secret) {
			t.Fatal("SOCKS parse error disclosed unsaved credentials or accepted malformed input")
		}
	}
}
