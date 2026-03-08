package identity

type DeviceIdentity struct {
	Pubkey []byte
	PrivateKey []byte
	ChainHead []byte
	CreatedAt int64
}

type SuccessionRecord struct {
    OldPubkey []byte
    NewPubkey []byte
    Timestamp int64
    Signature []byte
}