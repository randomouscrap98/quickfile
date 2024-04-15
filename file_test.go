package quickfile

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func uniqueFile(extension string) string {
	os.MkdirAll("ignore", 0600)
	return filepath.Join("ignore", fmt.Sprintf("%s%s", time.Now().Format("20060102_150405_000"), extension))
}

const DefaultUser = "testuser"

func createTables(t *testing.T) *Config {
	config := GetDefaultConfig()
	config.Accounts[DefaultUser] = nil
	config.ApplyDefaults()
	config.Datapath = uniqueFile(".db")
	err := CreateTables(&config)
	if err != nil {
		t.Fatalf("Couldn't create tables: %s\n")
	}
	return &config
}

func TestCreateTables(t *testing.T) {
	createTables(t)
}

func TestEmptyFind(t *testing.T) {
	config := createTables(t)
	result, err := GetFilesById([]int64{1, 2, 3}, config)
	if err != nil {
		t.Fatalf("Couldn't do an empty search: %s\n", err)
	}
	if len(result) != 0 {
		t.Fatalf("Result supposed to be empty!")
	}
}

func TestLive(t *testing.T) {
	// Try to get
}
