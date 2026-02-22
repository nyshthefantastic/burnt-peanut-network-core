package crypto

import (
	"crypto/ed25519"
	"testing"
)

func TestGenerateKeyPair(t *testing.T) {
	publicKey, privateKey, err := GenerateKeyPair()

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(publicKey) != ed25519.PublicKeySize {
		t.Fatalf("expected public key size %d, got %d", ed25519.PublicKeySize, len(publicKey))
	}
	if len(privateKey) != ed25519.PrivateKeySize {
		t.Fatalf("expected private key size %d, got %d", ed25519.PrivateKeySize, len(privateKey))
	}

}

func TestSign(t *testing.T) {
	_, privateKey, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	message := []byte("test message")
	signature, err := Sign(privateKey, message)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(signature) != ed25519.SignatureSize {
		t.Fatalf("expected signature size %d, got %d", ed25519.SignatureSize, len(signature))
	}
}

func TestVerifyWithWrongKey(t *testing.T) {
    message := []byte("test message")
    // 1. Generate TWO keypairs
    _, privateKey1, err := GenerateKeyPair()
    if err != nil {
        t.Fatalf("expected no error, got %v", err)
    }
    publicKey2, _, err := GenerateKeyPair()
    if err != nil {
        t.Fatalf("expected no error, got %v", err)
    }	
    // 2. Sign a message with keypair 1
    signature, err := Sign(privateKey1, message)
    if err != nil {
        t.Fatalf("expected no error, got %v", err)
    }
    // 3. Verify the signature using keypair 2's public key
    valid, err := Verify(publicKey2, message, signature)
    if err != nil {
        	t.Fatalf("expected no error, got %v", err)
    }
    if valid {
        t.Fatalf("expected false, got true")
    }
}


func TestSignAndVerify(t *testing.T) {
    message := []byte("test sign and verify message")
    publicKey, privateKey, err := GenerateKeyPair()
    if err != nil {
        t.Fatalf("expected no error, got %v", err)
    }
    signature, err := Sign(privateKey, message)
    if err != nil {
        t.Fatalf("expected no error, got %v", err)
    }
    valid, err := Verify(publicKey, message, signature)
    if err != nil {
        t.Fatalf("expected no error, got %v", err)
    }
    if !valid {
        t.Fatalf("expected true, got false")
    }
}

func TestVerifyWithTamperedMessage(t *testing.T) {
    // Generate keypair, sign "hello"
    publicKey, privateKey, err := GenerateKeyPair()
    if err != nil {
        t.Fatalf("expected no error, got %v", err)
    }
    initialMessage := []byte("hello")
    signature, err := Sign(privateKey, initialMessage)
    if err != nil {
        t.Fatalf("expected no error, got %v", err)
    }
    incorrectMessage := []byte("hallo world")
    valid, err := Verify(publicKey, incorrectMessage, signature)
    if err != nil {
        t.Fatalf("expected no error, got %v", err)
    }
    if valid {
        t.Fatalf("expected false, got true")
    }
}

func TestSignWithInvalidKey(t *testing.T) {
    message := []byte("test sign with invalid key message")
    invalidKey := make([]byte, 10)
    _, err := Sign(invalidKey, message)
    if err == nil {
        t.Fatalf("expected error, got nil")
    }
}