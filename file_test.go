package quickfile

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func uniqueFile(prefix, extension string) string {
	os.MkdirAll("ignore", 0600)
	return filepath.Join("ignore", fmt.Sprintf("%s_%s%s", time.Now().Format("20060102_150405.000"), prefix, extension))
}

const DefaultUser = "testuser"

func createTables(t *testing.T, name string) *Config {
	config := GetDefaultConfig()
	config.Accounts[DefaultUser] = nil
	config.ApplyDefaults()
	config.Datapath = uniqueFile(name, ".db")
	err := CreateTables(&config)
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
	fids, err := GetPaginatedFiles(0, config)
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

	fids, err = GetPaginatedFiles(0, config)
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
}
