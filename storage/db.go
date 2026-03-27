package storage

import (
	"database/sql"
	"errors"

	_ "github.com/mattn/go-sqlite3"
)

const (
	MaxReadOpenConns = 10
	MaxWriteOpenConns = 1
)

type Store struct {
	writer *sql.DB
	reader *sql.DB
}

func OpenDatabase(path string) (*Store, error) {
	/*
	we gonna need  2 database connection pools, one for writing and one for reading.

	SQLite is a file-based database. It only allow one writer at a time.
	as we need to support multiple readers, we need 2 connection pools.
	*/
	writer, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}

	writer.SetMaxOpenConns(MaxWriteOpenConns)
	_, err = writer.Exec("PRAGMA journal_mode=WAL")
	if err != nil {
		return nil, err
	}

	reader, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}
	reader.SetMaxOpenConns(MaxReadOpenConns)
	
	store := &Store{writer: writer, reader: reader}

	err = store.migrate()
	if err != nil {
		store.Close()
		return nil, err
	}

	return store, nil

}

func (s *Store) Close() error {
    writerErr := s.writer.Close()
    readerErr := s.reader.Close()
    return errors.Join(writerErr, readerErr)
}


func (s *Store) migrate() error {
    // This table tracks which migration version the database is at.
    _, err := s.writer.Exec(createSchemaVersionTableSQL)
    if err != nil {
        return err
    }

    // QueryRow returns a single row. Scan reads the value into a variable.
    var version int
    err = s.writer.QueryRow("SELECT version FROM schema_version").Scan(&version)
    
    if err == sql.ErrNoRows {
        // if No rows are tehere which means brand new database and version is 0
        version = 0
    } else if err != nil {
        return err
    }

    if version < 1 {
        err = s.runMigrationV1()
        if err != nil {
            return err
        }
    }

    return nil
}

func (s *Store) runMigrationV1() error {
    // Begin a transaction. if any of statement fails all the changes are rolled back.
    tx, err := s.writer.Begin()
    if err != nil {
        return err
    }
    /*
	 if we return early due to error the transaction is automatically rolled back. If we already committed Rollback does nothing.
	*/
    defer tx.Rollback()

    statements := []string{
        createShareRecordsTableSQL,
        indexShareRecordsSenderTableSQL,
        indexShareRecordsReceiverTableSQL,
        createPeersTableSQL,
        indexPeersTableSQL,
        createFilesTableSQL,
        indexFilesTableSQL,
        createCheckpointsTableSQL,
        indexCheckpointsTableSQL,
        createForkEvidenceTableSQL,
        indexForkEvidenceTableSQL,
        createTransferRequestsTableSQL,
        indexTransferRequestsTableSQL,
        createIdentityTableSQL,
    }

    for _, stmt := range statements {
        _, err = tx.Exec(stmt)
        if err != nil {
            return err
        }
    }

    _, err = tx.Exec("INSERT INTO schema_version (version) VALUES (1)")
    if err != nil {
        return err
    }

    return tx.Commit()
}


const createSchemaVersionTableSQL = `
CREATE TABLE IF NOT EXISTS schema_version (
    version INTEGER NOT NULL
)
`

const createShareRecordsTableSQL = `
CREATE TABLE IF NOT EXISTS share_records (
    id BLOB PRIMARY KEY,
    sender_pubkey BLOB NOT NULL,
    receiver_pubkey BLOB NOT NULL,
    prev_sender BLOB,
    prev_receiver BLOB,
    sender_record_index INTEGER NOT NULL,
    receiver_record_index INTEGER NOT NULL,
    sender_cumulative_sent INTEGER NOT NULL,
    sender_cumulative_received INTEGER NOT NULL,
    receiver_cumulative_sent INTEGER NOT NULL,
    receiver_cumulative_received INTEGER NOT NULL,
    request_hash BLOB NOT NULL,
    chunk_hashes BLOB,
    bytes_total INTEGER NOT NULL,
    timestamp INTEGER NOT NULL,
    sender_sig BLOB NOT NULL,
    receiver_sig BLOB NOT NULL,
    file_hash BLOB NOT NULL,
    visibility INTEGER NOT NULL DEFAULT 0
)`

const indexShareRecordsSenderTableSQL = `
CREATE INDEX IF NOT EXISTS idx_records_sender ON share_records(sender_pubkey);
`

const indexShareRecordsReceiverTableSQL = `
CREATE INDEX IF NOT EXISTS idx_records_receiver ON share_records(receiver_pubkey);
`


const createPeersTableSQL = `
CREATE TABLE IF NOT EXISTS peers (
    pubkey BLOB PRIMARY KEY,
    chain_head BLOB,
    record_index INTEGER NOT NULL DEFAULT 0,
    cumulative_sent INTEGER NOT NULL DEFAULT 0,
    cumulative_received INTEGER NOT NULL DEFAULT 0,
    last_seen INTEGER NOT NULL,
    has_fork_evidence INTEGER NOT NULL DEFAULT 0,
    transport_type TEXT
);
`

const indexPeersTableSQL = `
CREATE INDEX IF NOT EXISTS idx_peers_last_seen ON peers(last_seen);
`


const createFilesTableSQL = `
CREATE TABLE IF NOT EXISTS files (
    file_hash BLOB PRIMARY KEY,
    file_name TEXT NOT NULL,
    file_size INTEGER NOT NULL,
    chunk_size INTEGER NOT NULL,
    chunk_hashes BLOB,
    origin_pubkey BLOB NOT NULL,
    origin_sig BLOB NOT NULL,
    created_at INTEGER NOT NULL
);
`

const indexFilesTableSQL = `
CREATE INDEX IF NOT EXISTS idx_files_name ON files(file_name);
`

const createCheckpointsTableSQL = `
CREATE TABLE IF NOT EXISTS checkpoints (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    device_pubkey BLOB NOT NULL,
    chain_head BLOB NOT NULL,
    record_index INTEGER NOT NULL,
    cumulative_sent INTEGER NOT NULL,
    cumulative_received INTEGER NOT NULL,
    raw_balance INTEGER NOT NULL,
    timestamp INTEGER NOT NULL,
    device_sig BLOB NOT NULL,
    witnesses BLOB,
    confidence INTEGER NOT NULL DEFAULT 0
);
`

const indexCheckpointsTableSQL = `
CREATE INDEX IF NOT EXISTS idx_checkpoints_device ON checkpoints(device_pubkey, record_index);
`

const createForkEvidenceTableSQL = `
CREATE TABLE IF NOT EXISTS fork_evidence (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    device_pubkey BLOB NOT NULL,
    record_a BLOB NOT NULL,
    record_b BLOB NOT NULL,
    reporter_pubkey BLOB NOT NULL,
    reporter_sig BLOB NOT NULL,
    detected_at INTEGER NOT NULL
);
`

const indexForkEvidenceTableSQL = `
CREATE INDEX IF NOT EXISTS idx_forks_device ON fork_evidence(device_pubkey);
`

const createTransferRequestsTableSQL = `
CREATE TABLE IF NOT EXISTS transfer_requests (
    hash BLOB PRIMARY KEY,
    requester_pubkey BLOB NOT NULL,
    file_hash BLOB NOT NULL,
    chunk_indices BLOB,
    nonce BLOB NOT NULL,
    timestamp INTEGER NOT NULL,
    signature BLOB NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending'
);
`

const indexTransferRequestsTableSQL = `
CREATE INDEX IF NOT EXISTS idx_requests_timestamp ON transfer_requests(timestamp);
`

const createIdentityTableSQL = `
CREATE TABLE IF NOT EXISTS identity (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    pubkey BLOB NOT NULL,
    created_at INTEGER NOT NULL,
    chain_head BLOB,
    chain_index INTEGER NOT NULL DEFAULT 0,
    cumulative_sent INTEGER NOT NULL DEFAULT 0,
    cumulative_received INTEGER NOT NULL DEFAULT 0
);
`

