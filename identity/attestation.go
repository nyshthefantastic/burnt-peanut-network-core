package identity

import (
	"encoding/binary"
	"errors"
	"time"

	"github.com/nyshthefantastic/burnt-peanut-network-core/crypto"
)

/*

Android Play Integrity:

The native Android code calls Google's Play Integrity API
Google returns a signed JWT (JSON Web Token)
The JWT contains: device integrity verdict, app integrity, account details
Verification means: decode the JWT, check Google's signature, verify the claims

iOS App Attest:

The native iOS code calls Apple's App Attest API
Apple returns a CBOR-encoded attestation object with a certificate chain
Verification means: validate the certificate chain back to Apple's root CA, verify the assertion


we will not be verifying using google or apple. instead we will be verifying the attestation envelope.

*/


// we can use this to verify attestions received from other peers using their public key
func VerifyAttestation(attestationBlob []byte, pubkey []byte) (bool, error) {
    if len(attestationBlob) == 0 {
        return false, errors.New("attestation blob is empty")
    }
    if len(pubkey) == 0 {
        return false, errors.New("pubkey is empty")
    }

    envelope, err := parseAttestationEnvelope(attestationBlob)
    if err != nil {
        return false, err
    }

    if envelope.Type != AttestationAndroid && envelope.Type != AttestationIOS {
        return false, errors.New("unknown attestation type")
    }

    age := time.Now().Unix() - envelope.Timestamp
	// we will be expiring the attestation after 24 hours
    if age < 0 || age > 86400 {
        return false, errors.New("attestation expired or has future timestamp")
    }

    pubkeyHash := crypto.Hash(pubkey)
    if !bytesEqual(envelope.DevicePubkey, pubkeyHash[:]) {
        return false, errors.New("attestation pubkey mismatch")
    }

    if len(envelope.PlatformBlob) == 0 {
        return false, errors.New("platform attestation blob is empty")
    }

    return true, nil
}

func parseAttestationEnvelope(data []byte) (*AttestationEnvelope, error) {
    // Minimum size: 1 (type) + 8 (timestamp) + 32 (pubkey hash) = 41 bytes
    if len(data) < 41 {
        return nil, errors.New("attestation blob too short")
    }

    envelope := &AttestationEnvelope{
        Type:         AttestationType(data[0]),
        Timestamp:    int64(binary.BigEndian.Uint64(data[1:9])),
        DevicePubkey: data[9:41],
        PlatformBlob: data[41:],
    }

    return envelope, nil
}

func bytesEqual(a, b []byte) bool {
    if len(a) != len(b) {
        return false
    }
    for i := range a {
        if a[i] != b[i] {
            return false
        }
    }
    return true
}