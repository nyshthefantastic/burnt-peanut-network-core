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


type AttestationType uint8

const (
    AttestationUnknown AttestationType = iota;
    AttestationAndroid 
    AttestationIOS     
)


type AttestationEnvelope struct {
    Type          AttestationType
    Timestamp     int64
    DevicePubkey  []byte
    PlatformBlob  []byte  
}