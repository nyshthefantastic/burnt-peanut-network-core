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
	
	return &Store{writer: writer, reader: reader}, nil

}

func (s *Store) Close() error {
    writerErr := s.writer.Close()
    readerErr := s.reader.Close()
    return errors.Join(writerErr, readerErr)
}