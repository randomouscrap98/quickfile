package main

import (
	"database/sql"
	"fmt"
	"io"
	"mime"
	"path"
	"slices"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const (
	ChunkSize = 65536
	SqliteKey = "sqlite3"
)

type FileInsertMeta struct {
	Filename string
	Tags     []string
	Expire   time.Duration
	Account  string
}

type UploadFile struct {
	ID      int64
	Name    string
	Mime    string
	Account string
	Date    time.Time
	Expire  time.Time
	Tags    []string
	Length  int
}

// Function to generate placeholders for SQL query
func sliceToPlaceholder[T any](slice []T) string {
	placeholders := make([]rune, len(slice))
	for i := range placeholders {
		placeholders[i] = '?'
	}
	return fmt.Sprintf("%s", placeholders)
}

// Create the entire db structure from the given config. Safe to call repeatedly
func CreateTables(config *Config) error {
	db, err := sql.Open(SqliteKey, config.Datapath)
	if err != nil {
		return err
	}
	defer db.Close()

	allSql := []string{
		`CREATE TABLE IF NOT EXISTS meta (
      fid INTEGER PRIMARY KEY,
      name TEXT NOT NULL,
      account TEXT NOT NULL,
      mime TEXT NOT NULL,
      created DATETIME NOT NULL,
      expire DATETIME,
      length INT NOT NULL
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

// Get the (current) number of files for this user.
func GetUserFileCount(user string, config *Config) (int, error) {
	db, err := sql.Open(SqliteKey, config.Datapath)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	var count int
	err = db.QueryRow(
		"SELECT COUNT(*) FROM meta WHERE account = ? AND (expire IS NULL OR expire > ?)",
		user, time.Now(),
	).Scan(&count)
	return count, err
}

// Get the (current) total file size for this user
func GetUserFileSize(user string, config *Config) (int64, error) {
	db, err := sql.Open(SqliteKey, config.Datapath)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	var length int64
	err = db.QueryRow(
		"SELECT SUM(length) FROM meta WHERE f.account = ? AND (expire IS NULL OR expire > ?)",
		user, time.Now(),
	).Scan(&length)
	return length, err
}

// Get the (current) total file size overall (all files)
func GetTotalFileSize(config *Config) (int64, error) {
	db, err := sql.Open(SqliteKey, config.Datapath)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	var length int64
	err = db.QueryRow(
		"SELECT SUM(length) FROM meta WHERE (expire IS NULL OR expire > ?)",
		time.Now(),
	).Scan(&length)
	return length, err
}

// Check file upload for everything we possibly can before actually attempting the upload
func FilePrecheck(meta *FileInsertMeta, config *Config) (string, int64, error) {
	// Make sure the account exists
	acconf, ok := config.Accounts[meta.Account]
	if !ok {
		return "", 0, fmt.Errorf("not allowed to upload")
	}

	// Go out to the db and check how many files they have. If they're over, die
	fcount, err := GetUserFileCount(meta.Account, config)
	if err != nil {
		return "", 0, err
	}
	if fcount >= acconf.FileLimit {
		return "", 0, fmt.Errorf("too many files: %d", fcount)
	}

	// Find out the user's current total file usage
	length, err := GetUserFileSize(meta.Account, config)
	if err != nil {
		return "", 0, err
	}
	if length >= acconf.UploadLimit {
		return "", 0, fmt.Errorf("over total upload limit: %d", length)
	}

	// Check some other values for validity
	if Duration(meta.Expire) < acconf.MinExpire || Duration(meta.Expire) > acconf.MaxExpire {
		return "", 0, fmt.Errorf("invalid expire duration: %s -> %s", acconf.MinExpire, acconf.MaxExpire)
	}

	// Go figure out the mimetype and make sure it's valid (don't actually check the file)
	if meta.Filename == "" {
		return "", 0, fmt.Errorf("must provide filename")
	}

	extension := path.Ext(meta.Filename)
	if extension == "" {
		return "", 0, fmt.Errorf("filename must have extension")
	}

	mimeType := mime.TypeByExtension(extension)
	if mimeType == "" {
		return "", 0, fmt.Errorf("unknown mimetype")
	}

	if len(config.AllowedMimeTypes) != 0 {
		if slices.Index(config.AllowedMimeTypes, mimeType) < 0 {
			return "", 0, fmt.Errorf("mimetype not allowed: %s", mimeType)
		}
	}

	return mimeType, acconf.UploadLimit - length, nil
}

// Lookup a set of files by id. Get all information about them.
func GetFilesById(ids []int64, config *Config) (map[int64]UploadFile, error) {
	db, err := sql.Open(SqliteKey, config.Datapath)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	result := make(map[int64]UploadFile)

	/*
			ID      int64
			Name    string
			Mime    string
			Account string
		  Date time.Time
			Expire  time.Time
			Tags    []string
			Length  int
	*/

	s, err := db.Prepare(fmt.Sprintf("SELECT * FROM files WHERE id IN (%s)", sliceToPlaceholder(ids)))
	rows, err := s.Exec( //db.Query(
		//fmt.Sprintf("SELECT f.*, SUM(c.length) AS length, FROM files f JOIN chunks c ON f.fid = c.fid WHERE f.id IN (%s)", sliceToPlaceholder(ids)),
		ids...,
	)
	return result, err
}

// Insert tags for the given fid
func insertTags(fid int64, tags []string, tx *sql.Tx) error {
	// Insert all the tags (pretty simple)
	tagInsert, err := tx.Prepare("INSERT INTO tags(fid, tag) VALUES(?,?)")
	if err != nil {
		return err
	}
	defer tagInsert.Close()

	for _, tag := range tags {
		_, err = tagInsert.Exec(fid, tag)
		if err != nil {
			return err
		}
	}
	return nil
}

// Insert individual chunks for the given fid
func insertChunks(fid int64, file io.Reader, tx *sql.Tx, userRemaining int64, totalRemaining int64) (int64, error) {
	// Now insert the actual file data, one chunk at a time. After each chunk, check the
	// user's total file size
	chunk := make([]byte, ChunkSize)
	stillReading := true
	chunkInsert, err := tx.Prepare("INSERT INTO chunks(fid, length, data) VALUES(?,?,?)")
	if err != nil {
		return 0, err
	}
	defer chunkInsert.Close()

	totalLength := int64(0)

	for stillReading {
		len, err := io.ReadFull(file, chunk)
		if err != nil {
			if err == io.ErrUnexpectedEOF || err == io.EOF {
				chunk = chunk[:len]
				stillReading = false
			} else {
				return 0, err
			}
		}
		totalLength += int64(len)
		if userRemaining-totalLength < 0 {
			return 0, fmt.Errorf("out of user storage")
		}
		if totalRemaining-totalLength < 0 {
			return 0, fmt.Errorf("out of system storage")
		}
		_, err = chunkInsert.Exec(fid, len, chunk)
		if err != nil {
			return 0, err
		}
	}
	return totalLength, nil
}

// Perform the entire operation of inserting a file into the database, including all checks
// necessary to ensure valid operation
func InsertFile(meta *FileInsertMeta, file io.Reader, config *Config) (*UploadFile, error) {

	// Get safe filename, get extension, check mimetype, etc. Also checks
	// whether you're going to go over the length limit, etc (it does this while
	// inserting the file so we don't stream the whole file into memory)
	mimeType, dataRemaining, err := FilePrecheck(meta, config)
	if err != nil {
		return nil, err
	}

	// Go see how much space is left for us
	totalSize, err := GetTotalFileSize(config)
	if err != nil {
		return nil, err
	}
	totalRemaining := config.TotalUploadLimit - totalSize

	// Open the database file
	db, err := sql.Open(SqliteKey, config.Datapath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	// Get a transaction going
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Insert the main file entry
	sqlresult, err := tx.Exec(
		"INSERT INTO files(name, account, mime, created, expire) VALUES(?,?,?,?,?)",
		meta.Filename, meta.Account, mimeType, time.Now(), time.Now().Add(meta.Expire),
	)
	if err != nil {
		return nil, err
	}

	fid, err := sqlresult.LastInsertId()
	if err != nil {
		return nil, err
	}

	// Insert the tags
	err = insertTags(fid, meta.Tags, tx)
	if err != nil {
		return nil, err
	}

	// Insert the actual data!
	totalLength, err := insertChunks(fid, file, tx, dataRemaining, totalRemaining)
	if err != nil {
		return nil, err
	}

	// Now that we have the real length, update the thing
	_, err = tx.Exec("UPDATE files SET length = ? WHERE fid = ?", totalLength, fid)
	if err != nil {
		return nil, err
	}

	// We're good now
	tx.Commit()

	results, err := GetFilesById([]int64{fid}, config)
	if err != nil {
		return nil, err
	}
	result := results[fid]
	return &result, nil
}
