package agentproto

import (
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/chacha20poly1305"
)

const (
	Magic           = "DFAGENT1"
	ProtocolVersion = 1
	maxFrame        = 16 << 20
)

type Request struct {
	Version int               `json:"version"`
	ID      string            `json:"id"`
	Action  string            `json:"action"`
	Options map[string]string `json:"options,omitempty"`
	Secret  map[string]string `json:"secret,omitempty"`
}

type Response struct {
	Progress bool              `json:"progress,omitempty"`
	Version  int               `json:"version"`
	ID       string            `json:"id"`
	OK       bool              `json:"ok"`
	Error    string            `json:"error,omitempty"`
	Values   map[string]string `json:"values,omitempty"`
}

type Conn struct {
	reader io.Reader
	writer io.Writer
	aead   cipherAEAD
}

type cipherAEAD interface {
	NonceSize() int
	Seal([]byte, []byte, []byte, []byte) []byte
	Open([]byte, []byte, []byte, []byte) ([]byte, error)
}

func Client(reader io.Reader, writer io.Writer) (*Conn, error) {
	return handshake(reader, writer, true)
}

func Server(reader io.Reader, writer io.Writer) (*Conn, error) {
	return handshake(reader, writer, false)
}

func handshake(reader io.Reader, writer io.Writer, initiator bool) (*Conn, error) {
	private, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	local := private.PublicKey().Bytes()
	peer := make([]byte, len(local))
	writeHello := func() error {
		if _, err := writer.Write(append([]byte(Magic), local...)); err != nil {
			return err
		}
		return nil
	}
	readHello := func() error {
		magic := make([]byte, len(Magic))
		if _, err := io.ReadFull(reader, magic); err != nil {
			return err
		}
		if string(magic) != Magic {
			return errors.New("invalid dragfm agent handshake")
		}
		_, err := io.ReadFull(reader, peer)
		return err
	}
	if initiator {
		err = writeHello()
		if err == nil {
			err = readHello()
		}
	} else {
		err = readHello()
		if err == nil {
			err = writeHello()
		}
	}
	if err != nil {
		return nil, err
	}
	peerKey, err := ecdh.X25519().NewPublicKey(peer)
	if err != nil {
		return nil, err
	}
	shared, err := private.ECDH(peerKey)
	if err != nil {
		return nil, err
	}
	key, err := hkdf.Key(sha256.New, shared, nil, "dragfm-agent-v1", chacha20poly1305.KeySize)
	if err != nil {
		return nil, err
	}
	defer wipe(key)
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	return &Conn{reader: reader, writer: writer, aead: aead}, nil
}

func (c *Conn) Send(value any) error {
	plain, err := json.Marshal(value)
	if err != nil {
		return err
	}
	defer wipe(plain)
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	frame := append(nonce, c.aead.Seal(nil, nonce, plain, []byte(Magic))...)
	if len(frame) > maxFrame {
		return errors.New("agent frame too large")
	}
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(frame)))
	if _, err := c.writer.Write(length[:]); err != nil {
		return err
	}
	_, err = c.writer.Write(frame)
	return err
}

func (c *Conn) Receive(value any) error {
	var length [4]byte
	if _, err := io.ReadFull(c.reader, length[:]); err != nil {
		return err
	}
	size := binary.BigEndian.Uint32(length[:])
	if size < uint32(c.aead.NonceSize()) || size > maxFrame {
		return fmt.Errorf("invalid agent frame size %d", size)
	}
	frame := make([]byte, size)
	if _, err := io.ReadFull(c.reader, frame); err != nil {
		return err
	}
	nonce := frame[:c.aead.NonceSize()]
	plain, err := c.aead.Open(nil, nonce, frame[c.aead.NonceSize():], []byte(Magic))
	if err != nil {
		return err
	}
	defer wipe(plain)
	return json.Unmarshal(plain, value)
}

func wipe(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
