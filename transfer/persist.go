package transfer

import (
	"fmt"
	"time"

	"github.com/nyshthefantastic/burnt-peanut-network-core/storage"
)

func CheckpointTransferState(db *storage.Store, session *TransferSession) error {
	if db == nil {
		return fmt.Errorf("db is required")
	}
	if session == nil {
		return fmt.Errorf("session is required")
	}

	session.mu.Lock()
	row := storage.TransferStateRow{
		ID:        session.ID,
		PeerID:    session.PeerID,
		Direction: string(session.Direction),
		FileHash:  append([]byte(nil), session.FileHash...),
		State:     string(session.State),
		UpdatedAt: time.Now().Unix(),
	}
	if session.err != nil {
		row.LastError = session.err.Error()
	}
	session.mu.Unlock()

	return db.UpsertTransferState(row)
}

func RecoverSessions(db *storage.Store) ([]*TransferSession, error) {
	if db == nil {
		return nil, fmt.Errorf("db is required")
	}

	rows, err := db.GetAllTransferStates()
	if err != nil {
		return nil, err
	}

	sessions := make([]*TransferSession, 0, len(rows))
	for _, row := range rows {
		session := &TransferSession{
			ID:        row.ID,
			PeerID:    row.PeerID,
			Direction: SessionDirection(row.Direction),
			FileHash:  append([]byte(nil), row.FileHash...),
			State:     TransferState(row.State),
		}
		if row.LastError != "" {
			session.err = fmt.Errorf("%s", row.LastError)
		}
		sessions = append(sessions, session)
	}

	return sessions, nil
}
