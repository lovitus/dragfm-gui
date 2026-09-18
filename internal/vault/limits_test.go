package vault

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	appconfig "github.com/lovitus/dragfm-gui/internal/config"
	"golang.org/x/crypto/chacha20poly1305"
)

func encodedHeader(t *testing.T, header Header) []byte {
	t.Helper()
	data, err := json.Marshal(header)
	if err != nil {
		t.Fatal(err)
	}
	var result bytes.Buffer
	result.WriteString(magic)
	if err := binary.Write(&result, binary.BigEndian, uint32(len(data))); err != nil {
		t.Fatal(err)
	}
	result.Write(data)
	return result.Bytes()
}

func TestHeaderRejectsExcessiveUnauthenticatedKDFParameters(t *testing.T) {
	base := Header{Version: 1, Salt: make([]byte, 16), Nonce: make([]byte, chacha20poly1305.NonceSizeX), Time: 3, Memory: 64 * 1024, Threads: 2}
	if _, err := readHeader(bytes.NewReader(encodedHeader(t, base))); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Header){
		"time": func(h *Header) { h.Time = ^uint32(0) },
		"memory": func(h *Header) { h.Memory = ^uint32(0) },
		"threads": func(h *Header) { h.Threads = 255 },
		"nonce": func(h *Header) { h.Nonce = nil },
	} {
		t.Run(name, func(t *testing.T) {
			header := base
			mutate(&header)
			if _, err := readHeader(bytes.NewReader(encodedHeader(t, header))); err == nil {
				t.Fatal("unsafe header accepted")
			}
			store := &Store{Path: filepath.Join(t.TempDir(), FileName), Header: header}
			if err := store.Save([]byte("long-enough"), appconfig.NewDocument()); err == nil {
				t.Fatal("Save derived from unsafe header")
			}
		})
	}
}

func TestOversizedCiphertextRejectedBeforeKeyDerivation(t *testing.T) {
	header := Header{Version: 1, Salt: make([]byte, 16), Nonce: make([]byte, chacha20poly1305.NonceSizeX), Time: 1, Memory: 8 * 1024, Threads: 1}
	path := filepath.Join(t.TempDir(), FileName)
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	data := encodedHeader(t, header)
	if _, err := file.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(int64(len(data)) + maxVaultPayload + chacha20poly1305.Overhead + 1); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Open(path, []byte("long-enough")); err == nil || err.Error() != "vault ciphertext exceeds 64 MiB limit" {
		t.Fatalf("oversized vault: %v", err)
	}
}
