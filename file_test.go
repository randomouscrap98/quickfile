package quickfile

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/pelletier/go-toml/v2"
)

func uniqueFile(prefix, extension string) string {
	os.MkdirAll("ignore", 0700)
	return filepath.Join("ignore", fmt.Sprintf("%s_%s%s", time.Now().Format("20060102_150405.000"), prefix, extension))
}

const DefaultUser = "testuser"

func createTables(t *testing.T, name string) *Config {
	config_raw := GetDefaultConfig_Toml()
	var config Config
	err := toml.Unmarshal([]byte(config_raw), &config) //GetDefaultConfig()
	if err != nil {
		t.Fatalf("Couldn't parse config toml: %s\n", err)
	}
	config.Accounts[DefaultUser] = nil
	config.ApplyDefaults()
	config.Datapath = uniqueFile(name, ".db")
	err = CreateTables(&config)
	if err != nil {
		t.Fatalf("Couldn't create tables: %s\n", err)
	}
	return &config
}

func TestEmptyFind(t *testing.T) {
	config := createTables(t, "emptyfind")
	result, err := GetFilesById([]int64{1, 2, 3}, config)
	if err != nil {
		t.Fatalf("Couldn't do an empty search: %s\n", err)
	}
	if len(result) != 0 {
		t.Fatalf("Result supposed to be empty!")
	}
}

func workingMeta() FileInsertMeta {
	return FileInsertMeta{
		Account:  DefaultUser,
		Filename: "whatever.png",
		Expire:   time.Hour,
		Tags:     []string{"a", "tag", "yeah"},
	}
}

func randomizeArray(arr []byte) {
	for i := 0; i < len(arr); i++ {
		arr[i] = byte(rand.Intn(256)) //i & 0xFF)
	}
}

func TestVariousPrechecks(t *testing.T) {
	config := createTables(t, "prechecks")
	meta := workingMeta()
	// First, need to make sure a normal precheck passes
	mime, left, err := FilePrecheck(&meta, config)
	if err != nil {
		t.Fatalf("Default meta was supposed to work: %s\n", err)
	}
	if mime != "image/png" {
		t.Fatalf("Expected mime to be image/png, was %s\n", mime)
	}
	if left < config.DefaultUploadLimit {
		t.Fatalf("Expected to have %d space left, got %d\n", config.DefaultUploadLimit, left)
	}
	// Now all the weird failures
	meta = workingMeta()
	meta.Account = "notauser"
	mime, left, err = FilePrecheck(&meta, config)
	if err == nil {
		t.Fatalf("Should've failed because no user found!")
	}
	log.Printf("Expected error no user: %s\n", err)
	meta = workingMeta()
	meta.Filename = "whatever"
	mime, left, err = FilePrecheck(&meta, config)
	if err == nil {
		t.Fatalf("Should've failed because no extension/mime!")
	}
	log.Printf("Expected error no extension: %s\n", err)
	meta = workingMeta()
	meta.Expire = time.Millisecond
	mime, left, err = FilePrecheck(&meta, config)
	if err == nil {
		t.Fatalf("Should've failed because too short expire!")
	}
	log.Printf("Expected error expire: %s\n", err)
	meta = workingMeta()
	meta.Filename = "thing.css"
	mime, _, err = FilePrecheck(&meta, config)
	if err != nil {
		t.Fatalf("Error: %s", err)
	}
	if mime != "text/css; charset=utf-8" {
		t.Fatalf("Expected text/css; charset=utf-8, got %s", mime)
	}
	meta = workingMeta()
	meta.Filename = "thing.html" //This should convert to plain
	mime, _, err = FilePrecheck(&meta, config)
	if err != nil {
		t.Fatalf("Error: %s", err)
	}
	if mime != "text/plain; charset=utf-8" {
		t.Fatalf("Expected text/plain; charset=utf-8, got %s", mime)
	}
}

func TestLive(t *testing.T) {
	config := createTables(t, "live")
	meta := workingMeta()
	expectedData := []byte("Not exactly a png")
	file := bytes.NewBuffer(expectedData)
	result, err := InsertFile(&meta, file, config)
	if err != nil {
		t.Fatalf("Failed to insert file: %s", err)
	}
	if result.Name != meta.Filename {
		t.Fatalf("Filename doesn't match: %s vs %s\n", result.Name, meta.Filename)
	}
	if result.Account != meta.Account {
		t.Fatalf("Account doesn't match: %s vs %s\n", result.Account, meta.Account)
	}
	if len(result.Tags) != len(meta.Tags) {
		t.Fatalf("Tag amount doesn't match: %d vs %d\n", len(result.Tags), len(meta.Tags))
	}
	if result.Length != len(expectedData) {
		t.Fatalf("Recorded data length doesn't match: %d vs %d\n", result.Length, len(expectedData))
	}
	if result.ID <= 0 {
		t.Fatalf("Needed a nonzero id, got %d\n", result.ID)
	}
	// Now we spawn a reader and see if the data we pull is the same
	reader, err := OpenChunkReader(result.ID, config)
	if err != nil {
		t.Fatalf("Failed to open chunk reader: %s\n", err)
	}
	exactBuffer := make([]byte, len(expectedData))
	exactLen, err := io.ReadFull(reader, exactBuffer)
	if err != nil {
		t.Fatalf("Error while reading exact chunk: %s\n", err)
	}
	if exactLen != len(exactBuffer) {
		t.Fatalf("Read length doesn't match: %d vs %d\n", exactLen, len(exactBuffer))
	}
	if !bytes.Equal(exactBuffer, expectedData) {
		t.Fatalf("Reader produced different data\n")
	}
	// now we generate data that spans various sizes:
	// - Exactly 1 chunk
	// - Exactly 2 chunks
	// - 3 chunks with a bit in the third
	lengths := []int{ChunkSize, ChunkSize * 2, ChunkSize*2 + 69}
	for _, l := range lengths {
		expectedData = make([]byte, l)
		randomizeArray(expectedData)
		meta.Filename = fmt.Sprintf("file_%d.zip", l)
		meta.Tags = append(meta.Tags, fmt.Sprintf("extra:%d", l))
		meta.Unlisted = fmt.Sprintf("bucket_%d", l)
		file = bytes.NewBuffer(expectedData)
		result, err = InsertFile(&meta, file, config)
		if err != nil {
			t.Fatalf("Failed to insert %s: %s", meta.Filename, err)
		}
		if result.Length != l {
			t.Fatalf("Length not right for %s: %d vs %d", meta.Filename, result.Length, l)
		}
		if len(result.Tags) != len(meta.Tags) {
			t.Fatalf("Tag amount doesn't match for %s: %d vs %d\n", meta.Filename, len(result.Tags), len(meta.Tags))
		}
		// Now we do the reader stuff... again
		reader, err := OpenChunkReader(result.ID, config)
		if err != nil {
			t.Fatalf("Failed to open chunk reader for %s: %s\n", meta.Filename, err)
		}
		exactBuffer = make([]byte, len(expectedData))
		exactLen, err = io.ReadFull(reader, exactBuffer)
		if err != nil {
			t.Fatalf("Error while reading exact chunk for %s: %s\n", meta.Filename, err)
		}
		if exactLen != len(exactBuffer) {
			t.Fatalf("Read length doesn't match for %s: %d vs %d\n", meta.Filename, exactLen, len(exactBuffer))
		}
		if !bytes.Equal(exactBuffer, expectedData) {
			t.Fatalf("Reader produced different data for %s\n", meta.Filename)
		}
		log.Printf("Passed: %s\n", meta.Filename)
	}
}

func TestCleanup(t *testing.T) {

	const NUMCHUNKS = 100

	config := createTables(t, "cleanup")
	meta := workingMeta()
	config.Accounts[meta.Account].MinExpire = Duration(0)
	expectedData := make([]byte, ChunkSize*NUMCHUNKS)

	var err error

	// Insert a couple big ones
	randomizeArray(expectedData)
	meta.Filename = fmt.Sprintf("file_0.zip")
	meta.Expire = 0
	file0 := bytes.NewBuffer(expectedData)
	_, err = InsertFile(&meta, file0, config)
	if err != nil {
		t.Fatalf("Couldn't insert file 0: %s\n", err)
	}

	randomizeArray(expectedData)
	meta.Filename = fmt.Sprintf("file_1.zip")
	meta.Expire = 5 * time.Minute
	file1 := bytes.NewBuffer(expectedData)
	result1, err := InsertFile(&meta, file1, config)
	if err != nil {
		t.Fatalf("Couldn't insert file 1: %s\n", err)
	}

	// Ensure only the one is there
	fids, err := GetPaginatedFiles(0, config, "", "")
	if err != nil {
		t.Fatalf("Couldn't get file ids: %s\n", err)
	}
	if len(fids) != 1 {
		t.Fatalf("Expected 1 files after insert, got %d\n", len(fids))
	}

	files, err := GetFilesById(fids, config)
	if err != nil {
		t.Fatalf("Couldn't get files: %s\n", err)
	}
	if len(files) != 1 {
		t.Fatalf("Expected 1 files after insert, got %d\n", len(files))
	}

	for _, f := range files {
		if f.Length != len(expectedData) {
			t.Fatalf("File %s not right length: %d vs %d\n", f.Name, f.Length, len(expectedData))
		}
	}

	// Delete expired (only the second one)
	stats, err := CleanupExpired(config)
	if err != nil {
		t.Fatalf("Couldn't cleanup: %s\n", err)
	}

	if stats.DeletedFiles != 1 {
		t.Fatalf("Expected 1 deleted file, got %d\n", stats.DeletedFiles)
	}
	if stats.DeletedTags != int64(len(meta.Tags)) {
		t.Fatalf("Expected %d deleted tags, got %d\n", len(meta.Tags), stats.DeletedTags)
	}
	if stats.DeletedChunks != NUMCHUNKS {
		t.Fatalf("Expected %d deleted chunks, got %d\n", NUMCHUNKS, stats.DeletedChunks)
	}

	fids, err = GetPaginatedFiles(0, config, "", "")
	if err != nil {
		t.Fatalf("Couldn't get file ids: %s\n", err)
	}
	if len(fids) != 1 {
		t.Fatalf("Expected 1 file after expire, got %d\n", len(fids))
	}
	if fids[0] != result1.ID {
		t.Fatalf("Expected only file after expire to be %d, got %d\n", result1.ID, len(fids))
	}

	// Vacuum without threshold (no vacuum)
	config.VacuumThreshold = 0
	vstats, err := TryVacuum(config)
	if err != nil {
		t.Fatalf("Couldn't vacuum none: %s\n", err)
	}
	if vstats.Vacuumed {
		t.Fatalf("Wasn't supposed to vacuum!\n")
	}

	// Vacuum with threshold (WAY lower than the amount we deleted)
	config.VacuumThreshold = ChunkSize
	vstats, err = TryVacuum(config)
	if err != nil {
		t.Fatalf("Couldn't vacuum real: %s\n", err)
	}
	if !vstats.Vacuumed {
		t.Fatalf("Was supposed to vacuum!\n")
	}
	if vstats.OldSize < ChunkSize*NUMCHUNKS*2 {
		t.Fatalf("Bad old size calc: %d vs %d\n", vstats.OldSize, ChunkSize*NUMCHUNKS*2)
	}
	if vstats.NewSize > ChunkSize*NUMCHUNKS*2 || vstats.NewSize < ChunkSize*NUMCHUNKS {
		t.Fatalf("Bad new size calc: %d\n", vstats.NewSize)
	}

	// Since we're here, let's also test the seeking capabilities
	reader, err := openChunkReaderRaw(result1.ID, config)
	if err != nil {
		t.Fatalf("Couldn't open the chunk reader: %s\n", err)
	}

	// Read a bit, we'll be in the middle of chunk 1
	buf := make([]byte, 50)
	_, err = io.ReadFull(reader, buf)
	if err != nil {
		t.Fatalf("Couldn't read first chunk: %s\n", err)
	}

	if !bytes.Equal(buf, expectedData[:50]) {
		t.Fatalf("First 50 bytes didn't match\n")
	}
	if reader.Offset != 50 {
		t.Fatalf("Offset not 50: is %d\n", reader.Offset)
	}

	// Now seek to someplace in the middle of the second chunk
	n, err := reader.Seek(ChunkSize+900, io.SeekStart)
	if err != nil {
		t.Fatalf("Couldn't seek: %s\n", err)
	}
	if n != ChunkSize+900 {
		t.Fatalf("Wrong offset: %d vs %d\n", n, ChunkSize+900)
	}

	// Start reading another 50 bytes. Should be decent...
	_, err = io.ReadFull(reader, buf)
	if err != nil {
		t.Fatalf("Couldn't read in the middle somewhere: %s\n", err)
	}
	if !bytes.Equal(buf, expectedData[n:n+50]) {
		t.Fatalf("Middle 50 bytes didn't match\n")
	}

	// Now seek to end
	n, err = reader.Seek(0, io.SeekEnd)
	if err != nil {
		t.Fatalf("Couldn't seek to end: %s\n", err)
	}
	if n != int64(len(expectedData)) {
		t.Fatalf("End not expected: %d vs %d\n", n, len(expectedData))
	}
}

func TestConcurrentWrites(t *testing.T) {
	const Concurrency int = 16
	const Repeat int = 10
	config := createTables(t, "concurrentwrites")
	errors := make([]error, 0)
	var wg sync.WaitGroup
	var errmu sync.Mutex
	var confmu sync.Mutex
	adderr := func(err error) {
		errmu.Lock()
		errors = append(errors, err)
		errmu.Unlock()
	}
	wg.Add(Concurrency)
	// Spawn a crapload of goroutines that just constantly insert and delete
	for i := 0; i < Concurrency; i++ {
		go func(id int) {
			account := fmt.Sprintf("account_%d", id)
			confmu.Lock()
			config.Accounts[account] = nil
			config.ApplyDefaults()
			confmu.Unlock()
			meta := workingMeta()
			meta.Filename = fmt.Sprintf("file%d.png", id)
			meta.Account = account
			data := make([]byte, 1024) // hopefully this is enough...
			for j := 0; j < len(data); j++ {
				data[j] = byte(j & 0xFF)
			}
			for n := 0; n < Repeat; n++ {
				uf, err := InsertFile(&meta, bytes.NewReader(data), config)
				if err != nil {
					adderr(err)
					break
				}
				err = ExpireFile(uf.ID, config)
				if err != nil {
					adderr(err)
					break
				}
			}
			wg.Done()
		}(i)
	}
	// Wait for them to finish
	wg.Wait()
	// There should be no errors
	if len(errors) > 0 {
		t.Fatalf("Errors while concurrent write: %v", errors)
	}
}
