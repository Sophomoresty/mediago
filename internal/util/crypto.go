package util

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
)

// MD5 returns the lowercase hex md5 digest of s. Matches the Python source's
// Mooc_Crypt.md5(), used for request signing on several sites.
func MD5(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

func AESDecryptCBC(data, key, iv []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(data)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("ciphertext is not a multiple of block size")
	}
	mode := cipher.NewCBCDecrypter(block, iv)
	plaintext := make([]byte, len(data))
	mode.CryptBlocks(plaintext, data)
	return pkcs7Unpad(plaintext)
}

func AESDecryptECB(data, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(data)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("ciphertext is not a multiple of block size")
	}
	plaintext := make([]byte, len(data))
	bs := block.BlockSize()
	for i := 0; i < len(data); i += bs {
		block.Decrypt(plaintext[i:i+bs], data[i:i+bs])
	}
	return pkcs7Unpad(plaintext)
}

func RSAEncryptPKCS1(data []byte, pubKeyPEM string) (string, error) {
	block, _ := pem.Decode([]byte(pubKeyPEM))
	if block == nil {
		return "", fmt.Errorf("failed to decode PEM block")
	}
	var rsaPub *rsa.PublicKey
	switch block.Type {
	case "RSA PUBLIC KEY":
		// PKCS#1 format (BEGIN RSA PUBLIC KEY) — used by eoffcn/offcncloud.
		pub, err := x509.ParsePKCS1PublicKey(block.Bytes)
		if err != nil {
			return "", fmt.Errorf("parse PKCS1 public key: %w", err)
		}
		rsaPub = pub
	default:
		// PKCS#8 / SubjectPublicKeyInfo format (BEGIN PUBLIC KEY).
		pub, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return "", err
		}
		var ok bool
		rsaPub, ok = pub.(*rsa.PublicKey)
		if !ok {
			return "", fmt.Errorf("not an RSA public key")
		}
	}
	encrypted, err := rsa.EncryptPKCS1v15(rand.Reader, rsaPub, data)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(encrypted), nil
}

// AESEncryptCBC performs AES-CBC encryption with PKCS7 padding and returns
// base64-encoded ciphertext. This mirrors the Python AESEncrypt.aes_encrypt
// used by several sites.
func AESEncryptCBC(data, key, iv []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	padded := pkcs7Pad(data, aes.BlockSize)
	ciphertext := make([]byte, len(padded))
	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(ciphertext, padded)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func pkcs7Pad(data []byte, blockSize int) []byte {
	padding := blockSize - len(data)%blockSize
	padtext := make([]byte, padding)
	for i := range padtext {
		padtext[i] = byte(padding)
	}
	return append(data, padtext...)
}

// RandomAlphanumeric returns a random string of the given length using
// ASCII letters and digits. Matches Python's ''.join(random.sample(
// string.ascii_letters + string.digits, n)).
func RandomAlphanumeric(n int) string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	for i := range b {
		b[i] = chars[int(b[i])%len(chars)]
	}
	return string(b)
}

func pkcs7Unpad(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty data")
	}
	padding := int(data[len(data)-1])
	if padding == 0 || padding > aes.BlockSize || padding > len(data) {
		return nil, fmt.Errorf("invalid padding")
	}
	for i := len(data) - padding; i < len(data); i++ {
		if data[i] != byte(padding) {
			return nil, fmt.Errorf("invalid padding")
		}
	}
	return data[:len(data)-padding], nil
}

func Base64Decode(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}
