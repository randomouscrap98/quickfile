package main

import (
	"database/sql"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const (
	ChunkSize = 65536
)

type UploadFile struct {
	Name         string
	Mime         string
	OriginalName string
	Account      string
	Expire       time.Time
	Tags         []string
}

func CreateTables(file string) error {
	db, err := sql.Open("sqlite3", file)
	if err != nil {
		return err
	}
	defer db.Close()

	allSql := []string{
		`CREATE TABLE IF NOT EXISTS files (
      fid INTEGER PRIMARY KEY,
      name TEXT NOT NULL,
      originalName TEXT NOT NULL,
      mime TEXT NOT NULL,
      expire DATETIME
    );`,
		`CREATE TABLE IF NOT EXISTS tags (
      tid INTEGER PRIMARY KEY,
      fid INTEGER NOT NULL,
      tag TEXT NOT NULL
    );`,
		`CREATE TABLE IF NOT EXISTS chunks (
      cid INTEGER PRIMARY KEY,
      fid INTEGER NOT NULL,
      length INTEGER NOT NULL,
      data BLOB NOT NULL
    );`,
		`CREATE INDEX IF NOT EXISTS idx_files_expire ON files (expire)`,
		`CREATE INDEX IF NOT EXISTS idx_tags_fid ON tags (fid)`,
		`CREATE INDEX IF NOT EXISTS idx_tags_tag ON tags (tag)`,
		`CREATE INDEX IF NOT EXISTS idx_chunks_fid ON chunks (fid)`,
	}

	for _, sql := range allSql {
		_, err = db.Exec(sql)
		if err != nil {
			return err
		}
	}

	return nil
}
