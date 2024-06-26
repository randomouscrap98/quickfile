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
	ChunkSize       = 65536
	DatabaseVersion = "2"
)

type FileInsertMeta struct {
	Filename string
	Tags     []string
	Expire   time.Duration
	Account  string
	Unlisted string
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

// WARN: This reader assumes each chunk is the same size, up until the last!!
type ChunkReader struct {
	Db     *sql.DB
	Stmt   *sql.Stmt
	Buffer []byte
	Length int64 // Need the length for whence end
	Offset int64 // This is a read seeker now
	Fid    int64
}

// Open a special reader which reads data from the sqlite database
func openChunkReaderRaw(id int64, config *Config) (*ChunkReader, error) {
	var err error
	cr := &ChunkReader{Fid: id}
	cr.Db, err = config.OpenDb()
	if err != nil {
		return nil, err
	}
	err = cr.Db.QueryRow("SELECT length FROM meta WHERE fid = ?", id).Scan(&cr.Length)
	if err != nil {
		cr.Db.Close()
		return nil, err
	}
	cr.Stmt, err = cr.Db.Prepare("SELECT data FROM chunks WHERE fid = ? ORDER BY cid LIMIT 1 OFFSET ?")
	if err != nil {
		cr.Db.Close()
		return nil, err
	}
	return cr, nil
}

func OpenChunkReader(id int64, config *Config) (io.ReadSeekCloser, error) {
	return openChunkReaderRaw(id, config)
}

func (cr *ChunkReader) Read(out []byte) (int, error) {
	// If our buffer is empty, read the next chunk into it from the database
	if len(cr.Buffer) == 0 {
		err := cr.Stmt.QueryRow(cr.Fid, cr.Offset/ChunkSize).Scan(&cr.Buffer)
		if err != nil {
			if err == sql.ErrNoRows {
				// Something normal happened. Nothing in the buffer and nothing in the DB
				return 0, io.EOF
			} else {
				// Something really unexpected happened
				return 0, err
			}
		}
		// Need to skip an amount of bytes from the chunk
		cr.Buffer = cr.Buffer[cr.Offset%ChunkSize:]
	}
	// Getting here means we have something in the buffer. Copy as much as we can and
	// mutate the underlying buffer for future calls. This means sometimes read alignment
	// is bad and the next read is like 1 byte or something, but whatever
	copyLen := copy(out, cr.Buffer)
	cr.Buffer = cr.Buffer[copyLen:]
	cr.Offset += int64(copyLen)
	return copyLen, nil
}

func (cr *ChunkReader) Seek(offset int64, whence int) (int64, error) {
	// Immediately delete the old buffer, it's useless now
	cr.Buffer = make([]byte, 0)
	var relative int64
	switch whence {
	case io.SeekStart:
		relative = 0
	case io.SeekEnd:
		relative = cr.Length
	case io.SeekCurrent:
		relative = cr.Offset
	default:
		return 0, fmt.Errorf("invalid whence")
	}

	newPos := relative + offset
	if newPos < 0 { // I don't care if you're past the end, whatever
		return 0, fmt.Errorf("negative position")
	}

	cr.Offset = newPos
	return cr.Offset, nil
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
      unlisted TEXT NOT NULL DEFAULT "",
      length INT NOT NULL,
	  compression TEXT
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
		`CREATE TABLE IF NOT EXISTS sysvalues (
	  "key" TEXT PRIMARY KEY,
	  value TEXT
	);`,
		`CREATE INDEX IF NOT EXISTS idx_meta_expire_unlisted_account ON meta (expire,unlisted,account)`,
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

	_, err = db.Exec("INSERT OR IGNORE INTO sysvalues VALUES(?,?)", "version", DatabaseVersion)
	if err != nil {
		return err
	}

	return nil
}

func VerifyDatabase(config *Config) error {
	db, err := config.OpenDb()
	if err != nil {
		return err
	}
	defer db.Close()
	var dbVersion string
	err = db.QueryRow("SELECT value FROM sysvalues WHERE \"key\" = ?", "version").Scan(&dbVersion)
	if err != nil {
		return err
	}
	if dbVersion != DatabaseVersion {
		return fmt.Errorf("incompatible database version: expected %s, got %s", DatabaseVersion, dbVersion)
	}
	return nil
}

// Statistics on the cleanup
type CleanupStatistics struct {
	DeletedFiles  int64
	DeletedChunks int64
	DeletedTags   int64
}

func (cs *CleanupStatistics) Any() bool {
	return cs.DeletedFiles > 0 || cs.DeletedChunks > 0 || cs.DeletedTags > 0
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

// Immediately expire the file
func ExpireFile(id int64, config *Config) error {
	db, err := config.OpenDb()
	if err != nil {
		return err
	}
	defer db.Close()
	info, err := db.Exec("UPDATE meta SET expire=created WHERE fid = ?", id)
	if err != nil {
		return err
	}
	rows, err := info.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("not found: %d", id)
	}
	return nil
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
		extension = ".bin"
	}

	mimeType := mime.TypeByExtension(extension)
	mimeBase, mimeExtra := StringUpTo(";", mimeType)
	mimeRedirect, ok := config.MimeTypeRedirect[strings.Trim(mimeBase, " ")]
	if ok {
		mimeType = mimeRedirect + mimeExtra
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
		"INSERT INTO meta(name, account, mime, created, expire, length, unlisted) VALUES(?,?,?,?,?,?,?)",
		meta.Filename, meta.Account, mimeType, time.Now(), time.Now().Add(meta.Expire), 0, meta.Unlisted,
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
