package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"

	"golang.org/x/crypto/curve25519"
)

// GenerateKeyPair genera una coppia di chiavi X25519 per lo scambio Diffie-Hellman
func GenerateKeyPair() (privateKey, publicKey []byte, err error) {
	privateKey = make([]byte, curve25519.ScalarSize)
	if _, err := io.ReadFull(rand.Reader, privateKey); err != nil {
		return nil, nil, err
	}
	
	// Calcola la chiave pubblica: publicKey = privateKey * basepoint
	publicKey, err = curve25519.X25519(privateKey, curve25519.Basepoint)
	if err != nil {
		return nil, nil, err
	}
	
	return privateKey, publicKey, nil
}

// ComputeSharedSecret calcola il segreto condiviso tra due chiavi X25519
func ComputeSharedSecret(privateKey, peerPublicKey []byte) ([]byte, error) {
	sharedSecret, err := curve25519.X25519(privateKey, peerPublicKey)
	if err != nil {
		return nil, err
	}
	
	// Deriva una chiave AES a 256 bit dal segreto condiviso
	hash := sha256.Sum256(sharedSecret)
	return hash[:], nil
}

// EncryptMessage cripta un messaggio usando AES-GCM con la chiave fornita
func EncryptMessage(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	
	nonce := make([]byte, aesGCM.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	
	// Cifra il messaggio e prependi il nonce
	ciphertext := aesGCM.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

// DecryptMessage decripta un messaggio usando AES-GCM con la chiave fornita
func DecryptMessage(key, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	
	nonceSize := aesGCM.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, errors.New("ciphertext too short")
	}
	
	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := aesGCM.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, err
	}
	
	return plaintext, nil
}