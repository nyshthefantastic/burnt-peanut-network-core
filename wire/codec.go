package wire

import (
	"encoding/binary"
	"errors"
	"io"

	pb "github.com/nyshthefantastic/burnt-peanut-network-core/wire/gen"
	"google.golang.org/protobuf/proto"
)

const (
	MaxMessageSize   = 16 * 1024 * 1024 // 16MB
	LengthPrefixSize = 4
)

// EncodeEnvelope serializes an Envelope to bytes with a 4-byte length prefix.
func EncodeEnvelope(env *pb.Envelope) ([]byte, error) {
	if env == nil {
		return nil, errors.New("envelope is nil")
	}

	payload, err := proto.Marshal(env)
	if err != nil {
		return nil, err
	}

	if len(payload) > MaxMessageSize {
		return nil, errors.New("message exceeds max size")
	}

	frame := make([]byte, LengthPrefixSize+len(payload))
	binary.BigEndian.PutUint32(frame[:LengthPrefixSize], uint32(len(payload)))
	copy(frame[LengthPrefixSize:], payload)

	return frame, nil
}

// DecodeEnvelope deserializes bytes (with length prefix) into an Envelope.
func DecodeEnvelope(data []byte) (*pb.Envelope, error) {
	if len(data) < LengthPrefixSize {
		return nil, errors.New("data too short for length prefix")
	}

	length := binary.BigEndian.Uint32(data[:LengthPrefixSize])
	if int(length) > MaxMessageSize {
		return nil, errors.New("message exceeds max size")
	}

	if len(data) < LengthPrefixSize+int(length) {
		return nil, errors.New("data shorter than declared length")
	}

	payload := data[LengthPrefixSize : LengthPrefixSize+int(length)]
	env := &pb.Envelope{}
	if err := proto.Unmarshal(payload, env); err != nil {
		return nil, err
	}

	return env, nil
}

// ReadEnvelope reads a single length-prefixed envelope from a stream.
func ReadEnvelope(r io.Reader) (*pb.Envelope, error) {
	header := make([]byte, LengthPrefixSize)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, err
	}

	length := binary.BigEndian.Uint32(header)
	if int(length) > MaxMessageSize {
		return nil, errors.New("message exceeds max size")
	}

	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}

	env := &pb.Envelope{}
	if err := proto.Unmarshal(payload, env); err != nil {
		return nil, err
	}

	return env, nil
}

// WriteEnvelope writes a length-prefixed envelope to a stream.
func WriteEnvelope(w io.Writer, env *pb.Envelope) error {
	frame, err := EncodeEnvelope(env)
	if err != nil {
		return err
	}

	_, err = w.Write(frame)
	return err
}