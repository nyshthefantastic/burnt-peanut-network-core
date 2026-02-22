package crypto

import (
	"crypto/ecdh"
	"crypto/rand"
)

func GenerateSessionKeyPair() ([]byte, []byte, error) {
	privateKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}

	privateKeyBytes := privateKey.Bytes()
	publicKeyBytes := privateKey.PublicKey().Bytes()

	return publicKeyBytes, privateKeyBytes, nil

}


func DeriveSharedSecret(myPrivateKey []byte, peerPublicKey []byte) ([]byte, error) {

	newPrivateKey, err := ecdh.X25519().NewPrivateKey(myPrivateKey)
	if err != nil {
		return nil, err
	}

	newPeerPublicKey, err := ecdh.X25519().NewPublicKey(peerPublicKey)

	if err != nil {
		return nil, err
	}

	sharedSecret, err := newPrivateKey.ECDH(newPeerPublicKey)
	if err != nil {
		return nil, err
	}
	return sharedSecret, nil
}