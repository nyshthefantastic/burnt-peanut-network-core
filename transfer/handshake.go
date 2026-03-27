package transfer

import (
	"fmt"

	"github.com/nyshthefantastic/burnt-peanut-network-core/identity"
	"github.com/nyshthefantastic/burnt-peanut-network-core/wire"
	pb "github.com/nyshthefantastic/burnt-peanut-network-core/wire/gen"
)

func BuildHandshake(identity *identity.DeviceIdentity, policy pb.ServicePolicy, sessionID string) *pb.HandshakeMsg {
	msg := &pb.HandshakeMsg{
		SessionId:      []byte(sessionID),
		IdentityPubkey: nil,
		Policy:         policy,
	}
	if identity != nil {
		msg.IdentityPubkey = append([]byte(nil), identity.Pubkey...)
	}
	return msg
}

func ProcessHandshake(msg *pb.HandshakeMsg) (peerIdentityPubkey []byte, peerPolicy pb.ServicePolicy, err error) {
	if msg == nil {
		return nil, pb.ServicePolicy_POLICY_NONE, fmt.Errorf("handshake message is required")
	}
	if len(msg.GetSessionId()) == 0 {
		return nil, pb.ServicePolicy_POLICY_NONE, fmt.Errorf("handshake session id is required")
	}
	if len(msg.GetIdentityPubkey()) == 0 {
		return nil, pb.ServicePolicy_POLICY_NONE, fmt.Errorf("handshake identity pubkey is required")
	}
	if !isKnownPolicy(msg.GetPolicy()) {
		return nil, pb.ServicePolicy_POLICY_NONE, fmt.Errorf("unknown service policy: %v", msg.GetPolicy())
	}

	return append([]byte(nil), msg.GetIdentityPubkey()...), msg.GetPolicy(), nil
}

func NegotiatePolicy(localPolicy pb.ServicePolicy, peerPolicy pb.ServicePolicy) pb.ServicePolicy {
	// Stricter policy has the greater enum value in proto (NONE < LIGHT < STRICT).
	if peerPolicy > localPolicy {
		return peerPolicy
	}
	return localPolicy
}

func isKnownPolicy(policy pb.ServicePolicy) bool {
	switch policy {
	case pb.ServicePolicy_POLICY_NONE, pb.ServicePolicy_POLICY_LIGHT, pb.ServicePolicy_POLICY_STRICT:
		return true
	default:
		return false
	}
}

func EncodeHandshakeEnvelope(msg *pb.HandshakeMsg) ([]byte, error) {
	if msg == nil {
		return nil, fmt.Errorf("handshake message is nil")
	}
	env := &pb.Envelope{
		Payload: &pb.Envelope_Handshake{
			Handshake: msg,
		},
	}
	return wire.EncodeEnvelope(env)
}

func DecodeHandshakeEnvelope(data []byte) (*pb.HandshakeMsg, error) {
	env, err := wire.DecodeEnvelope(data)
	if err != nil {
		return nil, err
	}
	handshake := env.GetHandshake()
	if handshake == nil {
		return nil, fmt.Errorf("envelope does not contain handshake payload")
	}
	return handshake, nil
}
