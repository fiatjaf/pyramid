package operator

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/fiatjaf/pyramid/global"
)

const operatorShardKeyFileName = "operator_shard_key"

func operatorShardKeyPath() string {
	return filepath.Join(global.S.DataPath, operatorShardKeyFileName)
}

func loadOrCreateOperatorShardKey() ([]byte, error) {
	path := operatorShardKeyPath()
	data, err := global.ReadRestrictedKeyFile(path)
	if err == nil {
		hexstr := trimSpaceBytes(data)
		zero(data)
		key, err := hex.DecodeString(string(hexstr))
		zero(hexstr)
		if err != nil || len(key) != 32 {
			return nil, fmt.Errorf("invalid operator shard key in %s", path)
		}
		return key, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}

	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("failed to generate operator shard key: %w", err)
	}
	if err := global.WriteRestrictedKeyFile(path, []byte(hex.EncodeToString(key)+"\n")); err != nil {
		zero(key)
		return nil, err
	}
	return key, nil
}

func encryptShard(plain string) (string, error) {
	key, err := loadOrCreateOperatorShardKey()
	if err != nil {
		return "", err
	}
	defer zero(key)

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

func decryptShard(ciphertext string) (string, error) {
	key, err := loadOrCreateOperatorShardKey()
	if err != nil {
		return "", err
	}
	defer zero(key)

	raw, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("shard ciphertext: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	ns := gcm.NonceSize()
	if len(raw) < ns+gcm.Overhead() {
		return "", fmt.Errorf("shard ciphertext too short")
	}
	plain, err := gcm.Open(nil, raw[:ns], raw[ns:], nil)
	if err != nil {
		return "", fmt.Errorf("shard decrypt: %w", err)
	}
	return string(plain), nil
}

func looksLikePlaintextShard(s string) bool {
	if s == "" {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

func revealShard(stored string) (plain string, migrated bool, err error) {
	plain, err = decryptShard(stored)
	if err == nil {
		return plain, false, nil
	}
	if looksLikePlaintextShard(stored) {
		return stored, true, nil
	}
	return "", false, err
}

func trimSpaceBytes(b []byte) []byte {
	i, j := 0, len(b)
	for i < j && (b[i] == ' ' || b[i] == '\n' || b[i] == '\r' || b[i] == '\t') {
		i++
	}
	for j > i && (b[j-1] == ' ' || b[j-1] == '\n' || b[j-1] == '\r' || b[j-1] == '\t') {
		j--
	}
	out := make([]byte, j-i)
	copy(out, b[i:j])
	return out
}

func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
