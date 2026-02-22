package crypto

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
)

type Signature = []byte


 func GenerateKeyPair() (publicKey []byte, privateKey []byte, err error){
	publicKey, privateKey, err = ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}

	return publicKey, privateKey, nil
 }



func Sign(privateKey []byte, message []byte) (signature Signature, err error){
	if len(privateKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("expected key size %d, got %d", ed25519.PrivateKeySize, len(privateKey))
	}
	signature = ed25519.Sign(privateKey, message)
	return signature, nil
}

func Verify(publicKey []byte, message []byte, signature Signature) (valid bool, err error) {
	if len(publicKey) != ed25519.PublicKeySize {
		return false, fmt.Errorf("expected key size %d, got %d", ed25519.PublicKeySize, len(publicKey))
	}
	
	valid = ed25519.Verify(publicKey, message, signature)
	
	return valid, nil
}