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
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return "", err
	}
	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return "", fmt.Errorf("not an RSA public key")
	}
	encrypted, err := rsa.EncryptPKCS1v15(rand.Reader, rsaPub, data)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(encrypted), nil
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
