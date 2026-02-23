package wire

import (
	"encoding/binary"
	"fmt"
	"io"

	pb "github.com/nyshthefantastic/burnt-peanut-network-core/wire/gen"
	"google.golang.org/protobuf/proto"
)

const (
	// for the initial version, we will use a chunk size of 16MB
	MaxMessageSize = 16 * 1024 * 1024 
)
/*
Learning Notes : the purpose of this file is to format messages for transport over the wire and read and write them to the wire.

we cannot use JSON as its not the right format for transport over the wire. so we use protobuf.

protobuf converts structs to binary format.

when we are receiving multiple messages from the wire, we need to be able to differentiate between them.
so we use a length-prefixed format.

each message is prefixed with a 4-byte length field. and then the message itself.

*/

/*
Encode and Decode functions are used to convert the protobuf structs to and from raw bytes.

transfer engine will use ReadEnvelope/WriteEnvelope for live device-to-device connections over Bluetooth as bytes
arrive gradually over the wire.
*/


// Takes a struct and returns length prefixed bytes
func EncodeEnvelope(env *pb.Envelope) ([]byte, error) {
	if env == nil {
		return nil, fmt.Errorf("envelope is nil")
	}
	// Marshal takes a protobuf struct and returns the raw bytes
    payload, err := proto.Marshal(env)
    if err != nil {
        return nil, err
    }

	//4-byte header to store the length of the message
	header := make([]byte, 4)

    length := uint32(len(payload))

	if length > MaxMessageSize {
		return nil, fmt.Errorf("message length is greater than chunk size")
	}

	//put the length into the header.
	// this takes uint32 length value of the payload and converts it to 4 bytes value and puts it into the header slice.
    binary.BigEndian.PutUint32(header, length)

	//[4 bytes length][protobuf bytes]
    return append(header, payload...), nil
}


func DecodeEnvelope(data []byte) (*pb.Envelope, error) {
	totalMessageLength := len(data)

	if totalMessageLength < 4 {
		return nil, fmt.Errorf("data is too short to contain a length")
	}

	// the payload length is the first 4 bytes of the message.
	payloadLength := binary.BigEndian.Uint32(data[:4])

	if payloadLength > MaxMessageSize {
    return nil, fmt.Errorf("message length is greater than chunk size")
}

	if payloadLength !=uint32(totalMessageLength - 4) {
		return nil, fmt.Errorf("payload length does not match the total message length")
	}

	payload := data[4:4+payloadLength]

	// this create a new envelope struct.
	envelope := pb.Envelope{}

	// this takes the raw bytes payload and write into the envelope struct.
	err := proto.Unmarshal(payload, &envelope)

	if err != nil {
		return nil, err
	}

	return &envelope, nil
}


func WriteEnvelope(w io.Writer, env *pb.Envelope) error {
    // EncodeEnvelope gives the framed bytes of the struct.
	data, err := EncodeEnvelope(env)
	if err != nil {
		return err
	}
    //sends bytes to the stream
	_, err = w.Write(data)
	if err != nil {
		return err
	}
	return nil
}

func ReadEnvelope(r io.Reader) (*pb.Envelope, error) {

	header := make([]byte, 4)

/*
the difference between io.ReadFull and io.Read is that io.ReadFull keeps reading until ALL 4 bytes arrive.
io.Read will read only the available bytes.
*/
	_, err := io.ReadFull(r, header)
	if err != nil {
		return nil, err
	}

	payloadLength := binary.BigEndian.Uint32(header)
	
	if payloadLength > MaxMessageSize {
		return nil, fmt.Errorf("message length is greater than chunk size")
	}

	payload := make([]byte, payloadLength)

	_, err = io.ReadFull(r, payload)
	if err != nil {
		return nil, err
	}
	envelope := pb.Envelope{}

	err = proto.Unmarshal(payload, &envelope)
	if err != nil {
		return nil, err
	}

	return &envelope, nil
}