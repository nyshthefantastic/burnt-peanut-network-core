package identity

import (
	"encoding/binary"
	"time"

	crypto "github.com/nyshthefantastic/burnt-peanut-network-core/crypto"
	storage "github.com/nyshthefantastic/burnt-peanut-network-core/storage"
)


func NewIdentity(store *storage.Store) (*DeviceIdentity, error) {
	pubkey, privateKey, err := crypto.GenerateKeyPair()
	if err != nil {
		return nil, err
	}
	
	// Persist public identity metadata in storage; private key is returned to caller/runtime.
	createdAt := time.Now().Unix()
	err = store.InitIdentity(pubkey, privateKey, createdAt)
		if err != nil {
		return nil, err
	}

	return &DeviceIdentity{
		Pubkey: pubkey,
		PrivateKey: privateKey,
		CreatedAt: createdAt,
	}, nil
}

func LoadIdentity(store *storage.Store) (*DeviceIdentity, error) {
	identity, err := store.GetIdentity()
	if err != nil {
		return nil, err
	}

	return &DeviceIdentity{
		Pubkey: identity.Pubkey,
		PrivateKey: identity.PrivateKey,
		ChainHead: identity.ChainHead,
		CreatedAt: identity.CreatedAt,
	}, nil
}

func CreateSuccessionRecord(oldPrivateKey, oldPublicKey, newPublicKey []byte) (*SuccessionRecord, error) {

	timestamp := time.Now().Unix()

	message := generateSuccessionRecordMessage(oldPublicKey, newPublicKey, timestamp)

	signature, err := crypto.Sign(oldPrivateKey, message)
	if err != nil {
		return nil, err
	}

	return &SuccessionRecord{
		OldPubkey: oldPublicKey,
		NewPubkey: newPublicKey,
		Timestamp: timestamp,
		Signature: signature,
	}, nil
}

func VerifySuccessionRecord(record *SuccessionRecord) (bool, error) {
	message := generateSuccessionRecordMessage(record.OldPubkey, record.NewPubkey, record.Timestamp)
	ok, err := crypto.Verify(record.OldPubkey, message, record.Signature)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	return true, nil
}


func generateSuccessionRecordMessage(oldPublicKey, newPublicKey []byte, timestamp int64) []byte {

	// build message by appending raw bytes in a fixed order
	message := make([]byte, 0, len(oldPublicKey)+len(newPublicKey)+8)
	message = append(message, oldPublicKey...)
	message = append(message, newPublicKey...)

	// timestamp needs to be exactly 8 bytes (uint64)
	ts := make([]byte, 8)

	binary.BigEndian.PutUint64(ts, uint64(timestamp))
	message = append(message, ts...)

	return message
}