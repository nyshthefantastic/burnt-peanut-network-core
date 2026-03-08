package storage

import (
	"errors"
)

// we dont  have this in proto as its only used in the storage layer.
type Identity struct {
    Pubkey           []byte
    CreatedAt        int64
    ChainHead        []byte
    ChainIndex       uint64
    CumulativeSent   uint64
    CumulativeReceived uint64
}

// hardcoding the id to 1 as we only have one identity per device.
func (s *Store) InitIdentity(pubkey []byte, createdAt int64) error {
	if pubkey == nil {
		return errors.New("pubkey is required")
	}
	if createdAt <= 0 {
		return errors.New("createdAt is required")
	}
	_, err := s.writer.Exec("INSERT INTO identity (id, pubkey, created_at) VALUES (1, ?, ?)", pubkey, createdAt)
	return err
}



func (s *Store) GetIdentity() (*Identity, error) {
	row := s.reader.QueryRow("SELECT pubkey, created_at, chain_head, chain_index, cumulative_sent, cumulative_received FROM identity WHERE id = 1")

	var identity Identity

	err := row.Scan(&identity.Pubkey, &identity.CreatedAt, &identity.ChainHead, &identity.ChainIndex, &identity.CumulativeSent, &identity.CumulativeReceived)
	if err != nil {
		return nil, err
	}
	return &identity, nil
}

func (s *Store) UpdateChainHead(hash []byte, index uint64, cumSent uint64, cumRecv uint64) error {
	if hash == nil {
		return errors.New("hash is required")
	}

	_, err := s.writer.Exec("UPDATE identity SET chain_head = ?, chain_index = ?, cumulative_sent = ?, cumulative_received = ? WHERE id = 1", hash, index, cumSent, cumRecv)
	return err
}