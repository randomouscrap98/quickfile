package main

import (
	"database/sql"
	"fmt"
	"mime"
	"path"
	//"github.com/gosimple/slug"
	"io"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const (
	ChunkSize = 65536
)

type UploadFile struct {
	ID           int
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

type FileInsertMeta struct {
	Filename string
	Tags     []string
	Expire   time.Time
	Account  string
}

func InsertFile(meta *FileInsertMeta, file io.Reader, config *Config) (*UploadFile, error) {
	// Get safe filename, get extension, check mimetype, etc. Also checks
	// whether you're going to go over the length limit, etc (it does this while
	// inserting the file so we don't stream the whole file into memory)
	if meta.Filename == "" {
		return nil, fmt.Errorf("must provide filename")
	}

	extension := path.Ext(meta.Filename)
	if extension == "" {
		return nil, fmt.Errorf("filename must have extension")
	}

	mimeType := mime.TypeByExtension(extension)
	if mimeType == "" {
		return nil, fmt.Errorf("unknown mimetype")
	}

	if len(config.AllowedMimeTypes) != 0 {
		found := false
		for _, mt := range config.AllowedMimeTypes {
			if mt == mimeType {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("mimetype not allowed: %s", mimeType)
		}
	}
	//safeName := slug.Make(meta.Filename)

	return nil, nil
}
