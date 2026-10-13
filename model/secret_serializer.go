package model

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"gorm.io/gorm/schema"
)

// secretPrefix marks values encrypted by the "secret" serializer; anything
// without it is legacy plaintext and is returned as-is.
const secretPrefix = "enc:v1:"

var secretKey []byte

// SetSecretKey installs the AES-256 key used for fields tagged serializer:secret.
func SetSecretKey(key []byte) error {
	if len(key) != 32 {
		return errors.New("DATA_ENCRYPTION_KEY must decode to 32 bytes")
	}
	secretKey = key
	return nil
}

type secretSerializer struct{}

func init() {
	schema.RegisterSerializer("secret", secretSerializer{})
}

func (secretSerializer) Scan(ctx context.Context, field *schema.Field, dst reflect.Value, dbValue any) error {
	var raw string
	switch value := dbValue.(type) {
	case nil:
	case string:
		raw = value
	case []byte:
		raw = string(value)
	default:
		return fmt.Errorf("unsupported secret column type %T", dbValue)
	}
	plain, err := decryptSecret(raw)
	if err != nil {
		return err
	}
	return field.Set(ctx, dst, plain)
}

func (secretSerializer) Value(_ context.Context, _ *schema.Field, _ reflect.Value, fieldValue any) (any, error) {
	plain, _ := fieldValue.(string)
	return encryptSecret(plain)
}

func encryptSecret(plain string) (string, error) {
	// ponytail: no key means a non-SSO deployment storing nothing sensitive here; config.Load requires the key in SSO mode.
	if plain == "" || len(secretKey) == 0 {
		return plain, nil
	}
	gcm, err := secretAEAD()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return secretPrefix + base64.RawStdEncoding.EncodeToString(sealed), nil
}

func decryptSecret(raw string) (string, error) {
	encoded, ok := strings.CutPrefix(raw, secretPrefix)
	if !ok {
		return raw, nil
	}
	if len(secretKey) == 0 {
		return "", errors.New("encrypted secret found but DATA_ENCRYPTION_KEY is not set")
	}
	data, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("decode secret: %w", err)
	}
	gcm, err := secretAEAD()
	if err != nil {
		return "", err
	}
	if len(data) < gcm.NonceSize() {
		return "", errors.New("secret ciphertext too short")
	}
	plain, err := gcm.Open(nil, data[:gcm.NonceSize()], data[gcm.NonceSize():], nil)
	if err != nil {
		return "", fmt.Errorf("decrypt secret: %w", err)
	}
	return string(plain), nil
}

func secretAEAD() (cipher.AEAD, error) {
	block, err := aes.NewCipher(secretKey)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
