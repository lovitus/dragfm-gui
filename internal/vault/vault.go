package vault

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	appconfig "github.com/lovitus/dragfm-gui/internal/config"
	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

const (
	FileName        = "dragfm-gui.vault"
	magic           = "DFGUIV01"
	maxHeaderSize   = 1 << 20
	maxVaultPayload = 64 << 20
)

var ErrWrongPassword = errors.New("主密码错误或保险库损坏")

type Header struct {
	Version int    `json:"version"`
	Hint    string `json:"hint"`
	Salt    []byte `json:"salt"`
	Nonce   []byte `json:"nonce"`
	Time    uint32 `json:"time"`
	Memory  uint32 `json:"memory"`
	Threads uint8  `json:"threads"`
}

type Store struct {
	Path   string
	Header Header
}

func Locate(executable string) (string, error) {
	adjacent := filepath.Join(filepath.Dir(executable), FileName)
	system, err := systemPath()
	if err != nil {
		return "", err
	}
	return locatePaths(adjacent, system)
}

func locatePaths(adjacent, system string) (string, error) {
	if fileExists(adjacent) {
		return adjacent, nil
	}
	if fileExists(system) {
		return system, nil
	}
	if writableDirectory(filepath.Dir(adjacent)) {
		return adjacent, nil
	}
	if err := os.MkdirAll(filepath.Dir(system), 0700); err != nil {
		return "", err
	}
	return system, nil
}

func ReadHeader(path string) (Header, error) {
	file, err := os.Open(path)
	if err != nil {
		return Header{}, err
	}
	defer file.Close()
	return readHeader(file)
}

func Open(path string, password []byte) (*Store, appconfig.Document, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, appconfig.Document{}, err
	}
	defer file.Close()
	header, err := readHeader(file)
	if err != nil {
		return nil, appconfig.Document{}, err
	}
	ciphertext, err := io.ReadAll(io.LimitReader(file, maxVaultPayload+chacha20poly1305.Overhead+1))
	if err != nil {
		return nil, appconfig.Document{}, err
	}
	if len(ciphertext) > maxVaultPayload+chacha20poly1305.Overhead {
		return nil, appconfig.Document{}, errors.New("vault ciphertext exceeds 64 MiB limit")
	}
	key := derive(password, header)
	defer wipe(key)
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, appconfig.Document{}, err
	}
	plain, err := aead.Open(nil, header.Nonce, ciphertext, headerAAD(header))
	if err != nil {
		return nil, appconfig.Document{}, ErrWrongPassword
	}
	defer wipe(plain)
	var document appconfig.Document
	if err := json.Unmarshal(plain, &document); err != nil {
		return nil, appconfig.Document{}, fmt.Errorf("decode vault: %w", err)
	}
	if err := migrate(&document); err != nil {
		return nil, appconfig.Document{}, err
	}
	return &Store{Path: path, Header: header}, document, nil
}

func Create(path, hint string, password []byte, document appconfig.Document) (*Store, error) {
	if len(password) < 8 {
		return nil, errors.New("主密码至少需要 8 个字节")
	}
	if len([]rune(hint)) == 0 {
		return nil, errors.New("主密码提示不能为空")
	}
	header := Header{Version: 1, Hint: hint, Salt: make([]byte, 16), Nonce: make([]byte, chacha20poly1305.NonceSizeX), Time: 3, Memory: 64 * 1024, Threads: 2}
	if _, err := rand.Read(header.Salt); err != nil {
		return nil, err
	}
	if _, err := rand.Read(header.Nonce); err != nil {
		return nil, err
	}
	store := &Store{Path: path, Header: header}
	if err := store.Save(password, document); err != nil {
		return nil, err
	}
	return store, nil
}

func migrate(document *appconfig.Document) error {
	if document == nil {
		return errors.New("empty vault document")
	}
	switch document.Version {
	case 0:
		document.Version = 1
		if document.UI.Theme == "" {
			document.UI.Theme = "system"
		}
	case appconfig.CurrentVersion:
	default:
		return fmt.Errorf("unsupported vault version %d", document.Version)
	}
	return nil
}

func (s *Store) Save(password []byte, document appconfig.Document) error {
	if s == nil || s.Path == "" {
		return errors.New("vault store has no path")
	}
	document.Version = appconfig.CurrentVersion
	plain, err := json.Marshal(document)
	if err != nil {
		return err
	}
	defer wipe(plain)
	if len(plain) > maxVaultPayload {
		return errors.New("vault document exceeds 64 MiB limit")
	}
	header := s.Header
	if err := validateHeader(header); err != nil {
		return err
	}
	header.Nonce = make([]byte, chacha20poly1305.NonceSizeX)
	if _, err := rand.Read(header.Nonce); err != nil {
		return err
	}
	key := derive(password, header)
	defer wipe(key)
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return err
	}
	ciphertext := aead.Seal(nil, header.Nonce, plain, headerAAD(header))
	headerBytes, err := json.Marshal(header)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(s.Path), ".dragfm-vault-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0600); err != nil {
		return err
	}
	if err := restrictVaultFile(temporaryPath); err != nil {
		return err
	}
	if _, err := temporary.Write([]byte(magic)); err != nil {
		return err
	}
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(headerBytes)))
	if _, err := temporary.Write(size[:]); err != nil {
		return err
	}
	if _, err := temporary.Write(headerBytes); err != nil {
		return err
	}
	if _, err := temporary.Write(ciphertext); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := replaceVaultFile(temporaryPath, s.Path); err != nil {
		return err
	}
	committed = true
	s.Header = header
	return nil
}

func readHeader(reader io.Reader) (Header, error) {
	prefix := make([]byte, len(magic))
	if _, err := io.ReadFull(reader, prefix); err != nil {
		return Header{}, err
	}
	if string(prefix) != magic {
		return Header{}, errors.New("not a dragfm-gui vault")
	}
	var sizeBytes [4]byte
	if _, err := io.ReadFull(reader, sizeBytes[:]); err != nil {
		return Header{}, err
	}
	size := binary.BigEndian.Uint32(sizeBytes[:])
	if size == 0 || size > maxHeaderSize {
		return Header{}, errors.New("invalid vault header size")
	}
	data := make([]byte, size)
	if _, err := io.ReadFull(reader, data); err != nil {
		return Header{}, err
	}
	var header Header
	if err := json.Unmarshal(data, &header); err != nil {
		return Header{}, err
	}
	if err := validateHeader(header); err != nil {
		return Header{}, err
	}
	return header, nil
}

// Bound the unauthenticated header before deriving its key. Otherwise a
// malformed vault can exhaust RAM or CPU before AEAD authentication rejects it.
func validateHeader(header Header) error {
	if header.Version != 1 || len(header.Salt) != 16 || len(header.Nonce) != chacha20poly1305.NonceSizeX || header.Time < 1 || header.Time > 10 || header.Memory < 8*1024 || header.Memory > 256*1024 || header.Threads < 1 || header.Threads > 16 || len(header.Hint) > maxHeaderSize/2 {
		return errors.New("invalid or excessive vault header parameters")
	}
	return nil
}

func headerAAD(header Header) []byte {
	copy := header
	copy.Nonce = nil
	data, _ := json.Marshal(copy)
	return data
}

func derive(password []byte, header Header) []byte {
	return argon2.IDKey(password, header.Salt, header.Time, header.Memory, header.Threads, chacha20poly1305.KeySize)
}

func wipe(value []byte) {
	for i := range value {
		value[i] = 0
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func writableDirectory(path string) bool {
	file, err := os.CreateTemp(path, ".dragfm-write-test-*")
	if err != nil {
		return false
	}
	name := file.Name()
	_ = file.Close()
	_ = os.Remove(name)
	return true
}

func systemPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(base, "dragfm-gui", FileName), nil
	}
	return filepath.Join(base, "dragfm-gui", FileName), nil
}
