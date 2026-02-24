package storage

import (
	"errors"

	pb "github.com/nyshthefantastic/burnt-peanut-network-core/wire/gen"
)


func (s *Store) UpsertPeer(peer *pb.PeerInfo) error{
	if peer == nil {
		return errors.New("peer is required")
	}
	_, err := s.writer.Exec("INSERT INTO peers (pubkey, chain_head, record_index, cumulative_sent, cumulative_received, last_seen, has_fork_evidence, transport_type) VALUES (?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(pubkey) DO UPDATE SET chain_head = ?, record_index = ?, cumulative_sent = ?, cumulative_received = ?, last_seen = ?, has_fork_evidence = ?, transport_type = ?", peer.Pubkey, peer.ChainHead, peer.RecordIndex, peer.Totals.CumulativeSent, peer.Totals.CumulativeReceived, peer.LastSeen, peer.HasForkEvidence, peer.TransportType, peer.ChainHead, peer.RecordIndex, peer.Totals.CumulativeSent, peer.Totals.CumulativeReceived, peer.LastSeen, peer.HasForkEvidence, peer.TransportType)
	
	if err != nil {
		return err
	}
	return nil
}

func (s *Store) GetPeer(publicKey []byte) (*pb.PeerInfo, error){
	if publicKey == nil {
		return nil, errors.New("public key is required")
	}
	row := s.reader.QueryRow("SELECT pubkey, chain_head, record_index, cumulative_sent, cumulative_received, last_seen, has_fork_evidence, transport_type FROM peers WHERE pubkey = ?", publicKey)

	return scanPeer(row)
}


func (s *Store) GetAllPeers(limit int) ([]*pb.PeerInfo, error){
	if limit <= 0 {
		return nil, errors.New("limit must be positive")
	}

	rows, err := s.reader.Query("SELECT pubkey, chain_head, record_index, cumulative_sent, cumulative_received, last_seen, has_fork_evidence, transport_type FROM peers ORDER BY last_seen DESC LIMIT ?", limit)

	if err != nil {
		return nil, err
	}
	defer rows.Close()
	
	peers := make([]*pb.PeerInfo, 0)

	for rows.Next() {
		peer, err := scanPeer(rows)
		if err != nil {
			return nil, err
		}
		peers = append(peers, peer)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return peers, nil
}

func (s *Store) EvictLRU(limit int) error{
	if limit <= 0 {
		return errors.New("limit must be positive")
	}
	_, err := s.writer.Exec("DELETE FROM peers WHERE pubkey NOT IN (SELECT pubkey FROM peers ORDER BY last_seen DESC LIMIT ?)", limit)
	if err != nil {
		return err
	}
	if err != nil {
		return err
	}

	return nil
}



func scanPeer(scanner interface{ Scan(...any) error }) (*pb.PeerInfo, error){
	var peer pb.PeerInfo
	var cumulativeSent, cumulativeReceived uint64
	var lastSeen int64
	var hasForkEvidence bool
	var transportType string

	err := scanner.Scan(
		&peer.Pubkey,
		&peer.ChainHead,
		&peer.RecordIndex,
		&cumulativeSent,
		&cumulativeReceived,
		&lastSeen,
		&hasForkEvidence,
		&transportType,
	)

	if err != nil {
		return nil, err
	} 

	peer.Totals = &pb.CumulativeTotals{
		CumulativeSent: cumulativeSent,
		CumulativeReceived: cumulativeReceived,
	}

	peer.LastSeen = lastSeen
	peer.HasForkEvidence = hasForkEvidence
	peer.TransportType = transportType
	return &peer, nil
}