package assets

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io"
)

//go:embed payload/*.gz
var payloads embed.FS

func Available() bool {
	entries, err := payloads.ReadDir("payload")
	return err == nil && len(entries) >= 8
}

func LinuxAgent(architecture string) ([]byte, string, error) {
	return linuxPayload("dragfm-agent", architecture)
}

func LinuxHans(architecture string) ([]byte, string, error) {
	return linuxPayload("hans", architecture)
}

func linuxPayload(program, architecture string) ([]byte, string, error) {
	name := map[string]string{
		"x86_64": "amd64", "amd64": "amd64",
		"aarch64": "arm64", "arm64": "arm64",
		"i386": "386", "i686": "386", "386": "386",
		"armv7l": "arm", "armv7": "arm", "arm": "arm",
	}[architecture]
	if name == "" {
		return nil, "", fmt.Errorf("unsupported Linux helper architecture %q", architecture)
	}
	compressed, err := payloads.ReadFile("payload/" + program + "-linux-" + name + ".gz")
	if err != nil {
		return nil, "", err
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, "", err
	}
	payload, err := io.ReadAll(reader)
	if closeErr := reader.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, "", err
	}
	hash := sha256.Sum256(payload)
	return payload, hex.EncodeToString(hash[:]), nil
}
