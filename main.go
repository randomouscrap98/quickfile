package main

import (
   "os"
   "fmt"
   "log"
   "time"
	"net/http"

   "github.com/pelletier/go-toml/v2"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

const ConfigFile = "config.toml"

type AccountConfig struct {
   UploadLimit int
   FileLimit int
}

type Config struct {
   Timeout float64 // Seconds to timeout
   Port int    // The port obviously
   DefaultUploadLimit int // Size in bytes of account upload 
   DefaultFileLimit int // Default Amount of files per account
   Accounts map[string]AccountConfig // The accounts usable
}

func must(err error) {
   if err != nil {
      panic(err)
   }
}

func main() {
   var config Config

   // Read the config. It's OK if it doesn't exist
   configData,err := os.ReadFile(ConfigFile)
   if err != nil {
      log.Printf("WARN: Can't read config file: %s", err)
      config = Config {
         Timeout : 60,
         Port : 5007,
      }
   } else {
      // If the config exists, it MUST be parsable.
      err = toml.Unmarshal(configData, &config)
      must(err)
   }

	r := chi.NewRouter()
	r.Use(middleware.Logger)
   r.Use(middleware.Timeout(time.Duration(config.Timeout * float64(time.Second))))

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("welcome"))
	})
	http.ListenAndServe(fmt.Sprintf(":%d", config.Port), r)
}
