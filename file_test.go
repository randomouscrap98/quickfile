package quickfile

import (
	"bytes"
	"fmt"
	"io"
	"log"
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
}
