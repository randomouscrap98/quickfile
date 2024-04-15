package main

import (
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	//"github.com/gosimple/slug"
	"github.com/pelletier/go-toml/v2"
)

const (
	ConfigFile = "config.toml"
)

var (
	mutex sync.Mutex
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func main() {
	var config Config

	// Read the config. It's OK if it doesn't exist
	configData, err := os.ReadFile(ConfigFile)
	if err != nil {
		log.Printf("WARN: Can't read config file: %s", err)
		config = GetDefaultConfig()
	} else {
		// If the config exists, it MUST be parsable.
		err = toml.Unmarshal(configData, &config)
		must(err)
	}

	// Get all the defaults propogated
	config.ApplyDefaults()
	fmt.Printf("Listening on port %d\n", config.Port)

	// Create the upload folder
	//err = os.MkdirAll(config.DataFolder, os.ModePerm)
	must(CreateTables(config.Datapath))
	fmt.Printf("Data location: %s\n", config.Datapath)

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Timeout(time.Duration(config.Timeout * float64(time.Second))))

	r.Get("/", GetIndex)
	r.Post("/", PostIndex)

	http.ListenAndServe(fmt.Sprintf(":%d", config.Port), r)
}

// Generate a random name of only lowercase letters of the given length
func GetRandomName(length int) string {
	result := make([]byte, length)
	for i := 0; i < length; i++ {
		result[i] = byte(int('a') + rand.Intn(26))
	}
	return string(result)
}

// Generate a random name (including expiration) of only lowercase letters of given length
func GetRandomNameExpire(length int, expire time.Duration) string {
	return fmt.Sprintf("%s_%s", GetRandomName(length), time.Now().Add(expire).Format("200601021504"))
}

// Create a file from the given reader with the given expiration and return the
// path to the file (relative to the upload folder)
func SaveFile(extension string, file io.Reader, expire time.Duration) (string, error) {
	return "", nil
}

func GetIndex(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("welcome"))
}

func PostIndex(w http.ResponseWriter, r *http.Request) {
}
