package main

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/randomouscrap98/quickfile"

	"github.com/dustin/go-humanize"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/httprate"
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

func initConfig() *quickfile.Config {
	config := quickfile.GetDefaultConfig()
	// Read the config. It's OK if it doesn't exist
	configData, err := os.ReadFile(ConfigFile)
	if err != nil {
		log.Printf("WARN: Can't read config file: %s", err)
	} else {
		// If the config exists, it MUST be parsable.
		err = toml.Unmarshal(configData, &config)
		must(err)
	}
	// Get all the defaults propogated
	config.ApplyDefaults()
	must(quickfile.CreateTables(&config))
	return &config
}

// Initialize the baseline router and server, but don't actually set up any routes.
func initServer(config *quickfile.Config) (chi.Router, *http.Server) {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Timeout(time.Duration(config.Timeout)))
	r.Use(httprate.LimitByIP(config.RateLimitCount, time.Duration(config.RateLimitInterval)))
	s := &http.Server{
		Addr:           fmt.Sprintf(":%d", config.Port),
		Handler:        r,
		MaxHeaderBytes: config.HeaderLimit,
	}
	fmt.Printf("Listening on port %d, db = %s\n", config.Port, config.Datapath)
	fmt.Printf("Rate limit is %d per %s, timeout = %s\n",
		config.RateLimitCount, time.Duration(config.RateLimitInterval), time.Duration(config.Timeout))
	return r, s
}

func main() {
	config := initConfig()
	r, s := initServer(config)

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		data := make(map[string]any)
		data["account"] = ""
		data["time"] = time.Now().Format(time.RFC3339)
		data["defaultexpire"] = time.Duration(config.DefaultExpire)
		account, err := r.Cookie("account")
		if err == nil {
			_, ok := config.Accounts[account.Value]
			if ok {
				data["account"] = account.Value
			}
		}
		statistics, err := quickfile.GetFileStatistics("", config)
		if err != nil {
			log.Printf("WARN: couldn't get statistics: %s\n", err)
			data["statistics"] = &quickfile.FileStatistics{}
		} else {
			data["statistics"] = statistics
		}

		tmpl, err := template.New("index.html").Funcs(template.FuncMap{
			"Bytes":    humanize.Bytes,
			"BytesI64": func(n int64) string { return humanize.Bytes(uint64(n)) },
		}).ParseFiles("index.html")
		if err != nil {
			log.Printf("ERROR: can't load template: %s\n", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		err = tmpl.Execute(w, data)
		if err != nil {
			log.Printf("ERROR: can't execute template: %s\n", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
	})

	r.Post("/setuser", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, int64(config.SimpleFormLimit))
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Failed to parse form", http.StatusBadRequest)
			return
		}
		// Get form field value
		account := r.Form.Get("account")
		_, ok := config.Accounts[account]
		if ok {
			http.SetCookie(w, &http.Cookie{
				Name:  "account",
				Value: account,
			})
		} else {
			log.Printf("Bad user account attempt: %s", account)
		}
		// Redirect to the root of the application
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	r.Post("/upload", PostUpload)

	log.Fatal(s.ListenAndServe())
}

func PostUpload(w http.ResponseWriter, r *http.Request) {
}
