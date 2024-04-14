package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	//"github.com/gosimple/slug"
	"github.com/pelletier/go-toml/v2"
)

const ConfigFile = "config.toml"

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
	err = os.MkdirAll(config.DataFolder, os.ModePerm)
	must(err)
	fmt.Printf("Data folder: %s\n", config.DataFolder)

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Timeout(time.Duration(config.Timeout * float64(time.Second))))

	r.Get("/", GetIndex)
	r.Post("/", PostIndex)

	http.ListenAndServe(fmt.Sprintf(":%d", config.Port), r)
}

func GetIndex(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("welcome"))
}

func PostIndex(w http.ResponseWriter, r *http.Request) {
}
