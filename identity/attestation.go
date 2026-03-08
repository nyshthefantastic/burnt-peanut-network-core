package identity

import "errors"

func VerifyAttestation(attestationBlob []byte, publicKey []byte) (bool, error) {

  if len(attestationBlob) == 0 {
        return false, errors.New("attestation blob is empty")
    }
    if len(publicKey) == 0 {
        return false, errors.New("public key is empty")
    }
	
	// WIP

	

	return true, nil
}