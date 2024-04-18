package quickfile

import (
	"database/sql"
	"fmt"
	"io"
	"log"
	"mime"
	"path"
	"strings"
	"sync"
	"time"
)

const (
	ChunkSize = 65536
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

func (uf *UploadFile) IsExpired() bool {
	return uf.Expire.Before(time.Now())
}

type ChunkReader struct {
	Db        *sql.DB
	Stmt      *sql.Stmt
	Buffer    []byte
	CurrentId int64
	Fid       int64
}

// Open a special reader which reads data from the sqlite database
func OpenChunkReader(id int64, config *Config) (io.ReadCloser, error) {
	db, err := config.OpenDb()
	if err != nil {
		return nil, err
	}
	stmt, err := db.Prepare("SELECT cid, data FROM chunks WHERE fid = ? AND cid > ? LIMIT 1")
	if err != nil {
		db.Close()
		return nil, err
	}
	return &ChunkReader{Db: db, Stmt: stmt, Fid: id}, nil
}

func (cr *ChunkReader) Read(out []byte) (int, error) {
	// If our buffer is empty, read the next chunk into it from the database
	if len(cr.Buffer) == 0 {
		err := cr.Stmt.QueryRow(cr.Fid, cr.CurrentId).Scan(&cr.CurrentId, &cr.Buffer)
		if err != nil {
			if err != sql.ErrNoRows {
				// Something really unexpected happened
				return 0, err
			} else {
				// Something normal happened. Nothing in the buffer and nothing in the DB
				return 0, io.EOF
			}
		}
	}
	// Getting here means we have something in the buffer. Copy as much as we can and
	// mutate the underlying buffer for future calls. This means sometimes read alignment
	// is bad and the next read is like 1 byte or something, but whatever
	copyLen := copy(out, cr.Buffer)
	cr.Buffer = cr.Buffer[copyLen:]
	return copyLen, nil
}

func (cr *ChunkReader) Close() error {
	var err1, err2 error
	if cr.Stmt != nil {
		err1 = cr.Stmt.Close()
	} else {
		err1 = fmt.Errorf("statement not open")
	}
	if cr.Db != nil {
		err2 = cr.Db.Close()
	} else {
		err2 = fmt.Errorf("dbcon not open")
	}
	if err1 != nil {
		return err1
	}
	return err2
}

// Create the entire db structure from the given config. Safe to call repeatedly
func CreateTables(config *Config) error {
	db, err := config.OpenDb()
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
		`CREATE INDEX IF NOT EXISTS idx_meta_account_expire ON meta (account,expire)`,
		`CREATE INDEX IF NOT EXISTS idx_meta_expire ON meta (expire)`,
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

// Statistics on the cleanup
type CleanupStatistics struct {
	DeletedFiles  int64
	DeletedChunks int64
	DeletedTags   int64
}

var cleanupMutex sync.Mutex

// Remove expired images
func CleanupExpired(config *Config) (*CleanupStatistics, error) {
	cleanupMutex.Lock()
	defer cleanupMutex.Unlock()

	var cleanStats CleanupStatistics

	db, err := config.OpenDb()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	// Delete metadata immediately, this will make images inaccessible on the website
	// even if the chunks are left
	result, err := db.Exec("DELETE FROM meta WHERE expire IS NOT NULL and expire <= ?", time.Now())
	if err != nil {
		return nil, err
	}
	cleanStats.DeletedFiles, err = result.RowsAffected()
	if err != nil {
		log.Printf("WARN: Couldn't get number of deleted files: %s\n", err)
	}

	// Chunks go next, they're big
	result, err = db.Exec("DELETE FROM chunks WHERE fid NOT IN (select fid from meta)")
	if err != nil {
		return nil, err
	}
	cleanStats.DeletedChunks, err = result.RowsAffected()
	if err != nil {
		log.Printf("WARN: Couldn't get number of deleted chunks: %s\n", err)
	}

	// who cares about tags
	result, err = db.Exec("DELETE FROM tags WHERE fid NOT IN (select fid from meta)")
	if err != nil {
		return nil, err
	}
	cleanStats.DeletedTags, err = result.RowsAffected()
	if err != nil {
		log.Printf("WARN: Couldn't get number of deleted tags: %s\n", err)
	}

	return &cleanStats, nil
}

type VacuumStatistics struct {
	Vacuumed      bool
	OldStatistics *FileStatistics
	OldSize       int64
	NewSize       int64
}

// Attempt to vacuum the database (if it's necessary)
func TryVacuum(config *Config) (*VacuumStatistics, error) {
	cleanupMutex.Lock()
	defer cleanupMutex.Unlock()

	var err error
	result := &VacuumStatistics{}

	// Don't vacuum if not set
	if config.VacuumThreshold <= 0 {
		return result, nil
	}

	result.OldSize, err = config.DbSize()
	if err != nil {
		return nil, err
	}

	result.OldStatistics, err = GetFileStatistics("", config)
	if err != nil {
		return nil, err
	}

	if result.OldSize-result.OldStatistics.TotalSize > config.VacuumThreshold {
		db, err := config.OpenDb()
		if err != nil {
			return nil, err
		}
		defer db.Close()

		result.Vacuumed = true
		_, err = db.Exec("VACUUM")
		if err != nil {
			return nil, err
		}
		result.NewSize, err = config.DbSize()
		if err != nil {
			return nil, err
		}
	}

	return result, nil
}

func anyStartsWith(thing string, things []string) bool {
	for _, s := range things {
		if strings.HasPrefix(thing, s) {
			return true
		}
	}
	return false
}

// Check file upload for everything we possibly can before actually attempting the upload
func FilePrecheck(meta *FileInsertMeta, config *Config) (string, int64, error) {
	// Make sure the account exists
	acconf, ok := config.Accounts[meta.Account]
	if !ok {
		return "", 0, fmt.Errorf("not allowed to upload")
	}

	if len(meta.Tags) > config.MaxFileTags {
		return "", 0, fmt.Errorf("too many file tags. max: %d", config.MaxFileTags)
	}

	if len(meta.Filename) > config.MaxFileName {
		return "", 0, fmt.Errorf("filename too long! max: %d", config.MaxFileName)
	}

	// Go out to the db and check how many files they have. If they're over, die
	userStats, err := GetFileStatistics(meta.Account, config)
	if err != nil {
		return "", 0, err
	}
	if userStats.Count >= int64(acconf.FileLimit) {
		return "", 0, fmt.Errorf("too many files: %d", userStats.Count)
	}
	if userStats.TotalSize >= acconf.UploadLimit {
		return "", 0, fmt.Errorf("over total upload limit: %d", userStats.TotalSize)
	}

	// Check some other values for validity
	if Duration(meta.Expire) < acconf.MinExpire || Duration(meta.Expire) > acconf.MaxExpire {
		return "", 0, fmt.Errorf("invalid expire duration: %s -> %s",
			time.Duration(acconf.MinExpire), time.Duration(acconf.MaxExpire))
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
	mimeEnd := strings.Index(mimeType, ";")
	if mimeEnd >= 0 {
		mimeType = mimeType[:mimeEnd]
	}
	mimeRedirect, ok := config.MimeTypeRedirect[mimeType]
	if ok {
		mimeType = mimeRedirect
	}
	if mimeType == "" {
		return "", 0, fmt.Errorf("unknown mimetype")
	}

	if len(config.AllowedMimeTypes) != 0 {
		if !anyStartsWith(mimeType, config.AllowedMimeTypes) {
			return "", 0, fmt.Errorf("mimetype not allowed: %s", mimeType)
		}
	}
	if anyStartsWith(mimeType, config.ForbiddenMimeTypes) {
		return "", 0, fmt.Errorf("mimetype not allowed: %s", mimeType)
	}

	return mimeType, acconf.UploadLimit - userStats.TotalSize, nil
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
	sysstats, err := GetFileStatistics("", config)
	if err != nil {
		return nil, err
	}
	totalRemaining := config.TotalUploadLimit - sysstats.TotalSize

	// Open the database file
	db, err := config.OpenDb()
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
		"INSERT INTO meta(name, account, mime, created, expire, length) VALUES(?,?,?,?,?,?)",
		meta.Filename, meta.Account, mimeType, time.Now(), time.Now().Add(meta.Expire), 0,
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

	// Now that we have the real length, update the existing meta
	_, err = tx.Exec("UPDATE meta SET length = ? WHERE fid = ?", totalLength, fid)
	if err != nil {
		return nil, err
	}

	// We're good now
	tx.Commit()

	return GetFileById(fid, config)
}

// Insert tags for the given fid
func insertTags(fid int64, tags []string, tx *sql.Tx) error {
	// Insert all the tags (pretty simple)
	tagInsert, err := tx.Prepare("INSERT INTO tags(fid, tag) VALUES(?,?)")
	if err != nil {
		return err
	}
	defer tagInsert.Close()

	distinctTags := sliceDistinct(tags)
	for _, tag := range distinctTags {
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
		length, err := io.ReadFull(file, chunk)
		if err != nil {
			if err == io.ErrUnexpectedEOF || err == io.EOF {
				chunk = chunk[:length]
				stillReading = false
			} else {
				return 0, err
			}
		}
		// Do nothing for 0 length reads
		if length == 0 {
			continue
		}
		totalLength += int64(length)
		if userRemaining-totalLength < 0 {
			return 0, fmt.Errorf("out of user storage")
		}
		if totalRemaining-totalLength < 0 {
			return 0, fmt.Errorf("out of system storage")
		}
		_, err = chunkInsert.Exec(fid, length, chunk)
		if err != nil {
			return 0, err
		}
	}
	return totalLength, nil
}
