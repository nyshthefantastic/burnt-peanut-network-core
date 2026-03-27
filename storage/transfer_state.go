package storage

import "errors"

type TransferStateRow struct {
	ID        string
	PeerID    string
	Direction string
	FileHash  []byte
	State     string
	LastError string
	UpdatedAt int64
}

func (s *Store) UpsertTransferState(row TransferStateRow) error {
	if row.ID == "" {
		return errors.New("id is required")
	}
	if row.PeerID == "" {
		return errors.New("peer id is required")
	}
	if row.Direction == "" {
		return errors.New("direction is required")
	}
	if row.State == "" {
		return errors.New("state is required")
	}

	_, err := s.writer.Exec(`
		INSERT INTO transfer_state (id, peer_id, direction, file_hash, state, last_error, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			peer_id = excluded.peer_id,
			direction = excluded.direction,
			file_hash = excluded.file_hash,
			state = excluded.state,
			last_error = excluded.last_error,
			updated_at = excluded.updated_at
	`,
		row.ID,
		row.PeerID,
		row.Direction,
		row.FileHash,
		row.State,
		row.LastError,
		row.UpdatedAt,
	)
	return err
}

func (s *Store) GetAllTransferStates() ([]TransferStateRow, error) {
	rows, err := s.reader.Query(`
		SELECT id, peer_id, direction, file_hash, state, last_error, updated_at
		FROM transfer_state
		ORDER BY updated_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]TransferStateRow, 0)
	for rows.Next() {
		var row TransferStateRow
		if err := rows.Scan(
			&row.ID,
			&row.PeerID,
			&row.Direction,
			&row.FileHash,
			&row.State,
			&row.LastError,
			&row.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, row)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return out, nil
}
