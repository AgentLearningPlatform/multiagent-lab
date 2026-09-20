// Package secrets 提供 AES-256-GCM 加解密（模型连接 API Key 存储）。
// 密钥来自本地密钥文件（SECRET_KEY_FILE，默认 ./data/.secret），首启自动生成 32 字节。
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Box 持有 AES-256-GCM 密钥。
type Box struct {
	gcm cipher.AEAD
}

// LoadKeyFile 加载（必要时生成）密钥文件并构造 Box。
func LoadKeyFile(path string) (*Box, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
	}
	key, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, key, 0o600); err != nil {
			return nil, fmt.Errorf("write secret key file: %w", err)
		}
	} else if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("secret key file %s must contain exactly 32 bytes, got %d", path, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{gcm: gcm}, nil
}

// Encrypt 加密为 base64(nonce+ciphertext)。
func (b *Box) Encrypt(plain string) ([]byte, error) {
	nonce := make([]byte, b.gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	ct := b.gcm.Seal(nonce, nonce, []byte(plain), nil)
	return []byte(base64.StdEncoding.EncodeToString(ct)), nil
}

// Decrypt 解密 base64(nonce+ciphertext)。
func (b *Box) Decrypt(enc []byte) (string, error) {
	if len(enc) == 0 {
		return "", nil
	}
	raw, err := base64.StdEncoding.DecodeString(string(enc))
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}
	ns := b.gcm.NonceSize()
	if len(raw) < ns {
		return "", errors.New("ciphertext too short")
	}
	plain, err := b.gcm.Open(nil, raw[:ns], raw[ns:], nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}
	return string(plain), nil
}

// MaskKey 生成掩码显示：sk-****后4位（REQ-44）。
func MaskKey(key string) string {
	if key == "" {
		return ""
	}
	if len(key) <= 4 {
		return "****"
	}
	return "sk-****" + key[len(key)-4:]
}
