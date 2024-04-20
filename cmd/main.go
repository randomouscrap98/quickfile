package main

import (
	"fmt"
	"html/template"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	    "crypto/md5"
    "encoding/hex"

	"github.com/randomouscrap98/quickfile"

	"github.com/chi-middleware/proxy"
	"github.com/dustin/go-humanize"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/httprate"
	"github.com/pelletier/go-toml/v2"

	"runtime"
	"runtime/pprof"
)

const (
	ConfigFile      = "config.toml"
	AppVersion      = "0.2.1"
	DefaultUnlisted = "default"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func initConfig(allowRecreate bool) *quickfile.Config {
	var config quickfile.Config
	// Read the config. It's OK if it doesn't exist
	configData, err := os.ReadFile(ConfigFile)
	if err != nil {
		if allowRecreate {
			configRaw := quickfile.GetDefaultConfig_Toml()
			err = os.WriteFile(ConfigFile, []byte(configRaw), 0600)
			if err != nil {
				log.Printf("ERROR: Couldn't write default config: %s\n", err)
			} else {
				log.Printf("Generated default config at %s\n", ConfigFile)
				return initConfig(false)
			}
		} else {
			log.Fatalf("WARN: Couldn't read config file %s: %s", ConfigFile, err)
		}
	} else {
		// If the config exists, it MUST be parsable.
		err = toml.Unmarshal(configData, &config)
		must(err)
	}
	// Get all the defaults propogated
	config.ApplyDefaults()
	must(quickfile.CreateTables(&config))
	must(quickfile.VerifyDatabase(&config))
	return &config
}

// Retrieve the user account. Returns the name, the config, and whether it's valid
func getAccount(config *quickfile.Config, r *http.Request) (string, *quickfile.AccountConfig, bool) {
	account, err := r.Cookie(config.CookieName)
	if err == nil {
		acconf, ok := config.Accounts[account.Value]
		if ok {
			return account.Value, acconf, true
		}
	}
	return "", nil, false
}

// Initialize the baseline router and server, but don't actually set up any routes.
func initServer(config *quickfile.Config) (chi.Router, *http.Server) {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Timeout(time.Duration(config.Timeout)))
	r.Use(proxy.ForwardedHeaders())
	r.Use(httprate.LimitByIP(config.RateLimitCount, time.Duration(config.RateLimitInterval)))
	s := &http.Server{
		Addr:           fmt.Sprintf(":%d", config.Port),
		Handler:        r,
		MaxHeaderBytes: config.HeaderLimit,
	}
	log.Printf("Listening on port %d, db = %s\n", config.Port, config.Datapath)
	log.Printf("Rate limit is %d per %s, timeout = %s\n",
		config.RateLimitCount, time.Duration(config.RateLimitInterval), time.Duration(config.Timeout))
	return r, s
}

// Generate the data used for base template data
func getBaseTemplateData(config *quickfile.Config, r *http.Request) map[string]any {
	params := r.URL.Query()
	errors := make([]string, 0)
	data := make(map[string]any)
	data["appversion"] = AppVersion
	data["account"] = ""
	data["loggedin"] = false
	page, _ := strconv.Atoi(params.Get("page"))
	if page < 1 {
		page = 1
	}
	data["page"] = page
	data["time"] = time.Now()
	data["defaultexpire"] = time.Duration(config.DefaultExpire)
	account, acconf, ok := getAccount(config, r)
	if ok {
		data["account"] = account
		data["loggedin"] = true
		data["acconf"] = acconf
		data["userfiles"] = getPaginated(page, config, account, errors)
		userstatistics, err := quickfile.GetFileStatistics(account, config)
		if err != nil {
			log.Printf("WARN: couldn't get user statistics: %s\n", err)
			data["statistics"] = &quickfile.FileStatistics{}
		} else {
			data["userstatistics"] = userstatistics
		}
	}
	statistics, err := quickfile.GetFileStatistics("", config)
	if err != nil {
		log.Printf("WARN: couldn't get statistics: %s\n", err)
		data["statistics"] = &quickfile.FileStatistics{}
	} else {
		data["statistics"] = statistics
	}
	pagecount := int(math.Ceil(float64(statistics.Count) / float64(config.ResultsPerPage)))
	pagelist := make([]int, pagecount)
	for i := 0; i < pagecount; i++ {
		pagelist[i] = i + 1
	}
	data["pagecount"] = pagecount
	data["pagelist"] = pagelist
	data["files"] = getPaginated(page, config, "", errors)
	data["errors"] = errors
	return data
}

func getPaginated(page int, config *quickfile.Config, account string, errors []string) []*quickfile.UploadFile {
	unlisted := ""
	if account != "" {
		unlisted = DefaultUnlisted
	}
	fids, err := quickfile.GetPaginatedFiles(page-1, config, unlisted, account)
	if err != nil {
		log.Printf("WARN: couldn't load paginated ids: %s\n", err)
		errors = append(errors, "Couldn't load results, pagination error")
	} else {
		files := make([]*quickfile.UploadFile, 0, len(fids)) // just in case
		results, err := quickfile.GetFilesById(fids, config)
		if err != nil {
			log.Printf("WARN: couldn't load results from ids: %s\n", err)
			errors = append(errors, "Couldn't load results, lookup error")
		} else {
			for _, id := range fids {
				files = append(files, results[id])
			}
		}
		return files
	}
	return nil
}

func parseTags(tags string) []string {
	cleaned := strings.ReplaceAll(tags, ",", " ")
	splittags := strings.Split(cleaned, " ")
	result := make([]string, 0, len(splittags))
	for _, tag := range splittags {
		if tag != "" {
			result = append(result, tag)
		}
	}
	return result
}

// func getFileLinkName(f *quickfile.UploadFile) string {
// 	return url.PathEscape(f.Name)
// }

func getFileLink(f *quickfile.UploadFile) string {
	name := url.PathEscape(f.Name) // getFileLinkName(f)
	return fmt.Sprintf("file/%d/%s", f.ID, name)
}

func getIndexTemplate(_ *quickfile.Config) (*template.Template, error) {
	return template.New("index.html").Funcs(template.FuncMap{
		"Bytes":      humanize.Bytes,
		"BytesI":     func(n int) string { return humanize.Bytes(uint64(n)) },
		"BytesI64":   func(n int64) string { return humanize.Bytes(uint64(n)) },
		"NiceDate":   func(t time.Time) string { return t.UTC().Format(time.RFC3339) },
		"Until":      func(t time.Time) string { return strings.Trim(humanize.RelTime(t, time.Now(), "in the past", ""), " ") },
		"NotTooLong": func(t time.Time) bool { return t.Before(time.Now().AddDate(50, 0, 0)) },
		"arr":        func(els ...any) []any { return els },
		"FileLink":   getFileLink,
	}).ParseFiles("index.html")
}

func maintenanceFunc(config *quickfile.Config) {
	ticker := time.NewTicker(time.Duration(config.MaintenanceInterval))
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			cleanstats, err := quickfile.CleanupExpired(config)
			if err != nil {
				log.Printf("MAINTENANCE CLEANUP ERROR: %s\n", err)
			} else {
				log.Printf("Maintenance deleted: %d files, %d tags, %d chunks",
					cleanstats.DeletedFiles, cleanstats.DeletedTags, cleanstats.DeletedChunks)
			}
			vacuumstats, err := quickfile.TryVacuum(config)
			if err != nil {
				log.Printf("MAINTENANCE VACUUM ERROR: %s\n", err)
			} else {
				if vacuumstats.Vacuumed {
					log.Printf("Vacuum saved %d bytes\n", vacuumstats.OldSize-vacuumstats.NewSize)
				}
			}
		}
	}
}

func main() {
	log.Printf("Quickfile server version %s\n", AppVersion)
	config := initConfig(true)
	r, s := initServer(config)

	go maintenanceFunc(config)

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		data := getBaseTemplateData(config, r)
		tmpl, err := getIndexTemplate(config)
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

	r.Get("/file/{id}/{name}", func(w http.ResponseWriter, r *http.Request) {
		idraw := chi.URLParam(r, "id")
		id, err := strconv.ParseInt(idraw, 10, 64)
		if err != nil {
			http.Error(w, "Bad file ID format", http.StatusBadRequest)
			return
		}
		name := chi.URLParam(r, "name")
		fileinfo, err := quickfile.GetFileById(id, config)
		if err != nil || fileinfo.IsExpired() {
			http.Error(w, fmt.Sprintf("Can't find file %d", id), http.StatusNotFound)
			return
		}
		requestedName,err := url.PathUnescape(name)
		if err != nil {
			log.Printf("Path unescape failed for file lookup: %s\n", err)
		}
		//linkname := getFileLinkName(fileinfo)
		if err != nil || requestedName != fileinfo.Name {
			log.Printf("File lookup for %d bad name: '%s' vs '%s'", id, requestedName, fileinfo.Name)
			http.Error(w, fmt.Sprintf("Can't find file %d (bad name?)", id), http.StatusNotFound)
			return
		}
		reader, err := quickfile.OpenChunkReader(id, config)
		if err != nil {
			http.Error(w, fmt.Sprintf("Can't find file data %d (this is weird)", id), http.StatusNotFound)
			return
		}
		filenameHash := md5.Sum([]byte(fileinfo.Name))
		filenameHex := hex.EncodeToString(filenameHash[:])
		w.Header().Set("Etag", fmt.Sprintf("\"quickfile%s_%d_%s\"", AppVersion, fileinfo.ID, filenameHex))
		w.Header().Set("Cache-Control", fmt.Sprintf("max-age=%d", int64(time.Duration(config.CacheTime).Seconds())))
		http.ServeContent(w, r, fileinfo.Name, fileinfo.Date, reader)
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
				Name:  config.CookieName,
				Value: account,
			})
		} else {
			log.Printf("Bad user account attempt: %s", account)
		}
		// Redirect to the root of the application
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	r.Post("/upload", func(w http.ResponseWriter, r *http.Request) {
		// First, get the user, need to be logged in!
		account, _, ok := getAccount(config, r)
		if !ok {
			log.Printf("Upload attempt without an account\n")
			http.Error(w, "Invalid account", http.StatusUnauthorized)
			return
		}
		// Set limits on the body
		r.Body = http.MaxBytesReader(w, r.Body, int64(config.UploadSizeLimit))
		// Parse the multipart form. Allow small forms to go into memory (larger ones
		// go onto the filesystem, which is fine considering what we're doing with them)
		err := r.ParseMultipartForm(quickfile.ChunkSize)
		if err != nil {
			log.Printf("Can't parse multipart form: %s\n", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		expireRaw := strings.Trim(r.FormValue("expire"), " ")
		if expireRaw == "" {
			expireRaw = quickfile.ForeverDuration
		}
		expire, err := time.ParseDuration(expireRaw)
		if err != nil {
			http.Error(w, fmt.Sprintf("Couldn't parse expire: %s", err), http.StatusBadRequest)
			return
		}
		tags := parseTags(r.FormValue("tags"))
		unlisted := r.FormValue("unlisted")
		// We support multi-file upload, but every file gets the same expire and tags
		files := r.MultipartForm.File["files"]
		// Iterate over each file
		for _, fileHeader := range files {
			file, err := fileHeader.Open()
			if err != nil {
				log.Printf("Can't open one of the files in multipart form: %s\n", err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			defer file.Close()
			meta := quickfile.FileInsertMeta{
				Filename: fileHeader.Filename,
				Account:  account,
				Tags:     tags,
				Expire:   expire,
				Unlisted: unlisted,
			}
			upload, err := quickfile.InsertFile(&meta, file, config)
			if err != nil {
				log.Printf("Can't insert file %s: %s\n", meta.Filename, err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			} else {
				log.Printf("User %s uploaded file %s (ID: %d, %s)\n", upload.Account, upload.Name, upload.ID, humanize.Bytes(uint64(upload.Length)))
			}
		}
		// Now that we're done, redirect back to the main page
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	r.Post("/delete/{id}", func(w http.ResponseWriter, r *http.Request) {
		idraw := chi.URLParam(r, "id")
		id, err := strconv.ParseInt(idraw, 10, 64)
		if err != nil {
			http.Error(w, "Bad file ID format", http.StatusBadRequest)
			return
		}
		user, _, exists := getAccount(config, r)
		if !exists {
			log.Printf("Delete attempt without an account\n")
			http.Error(w, "Invalid account", http.StatusUnauthorized)
			return
		}
		file, err := quickfile.GetFileById(id, config)
		if err != nil {
			log.Printf("Delete file lookup error: %s\n", err)
			http.Error(w, "File lookup error", http.StatusNotFound)
			return
		}
		if file.Account != user {
			log.Printf("Delete attempt account mismatch: %s deleting %s\n", user, file.Account)
			http.Error(w, "Invalid account", http.StatusUnauthorized)
			return
		}
		// Yes, it is known that you can repeatedly "delete" a file while it still
		// exists on the server. I don't think it's an issue
		err = quickfile.ExpireFile(id, config)
		if err != nil {
			log.Printf("Delete error on %d: %s\n", id, err)
			http.Error(w, "Error on delete", http.StatusBadRequest)
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	if config.MemProfileFile != "" {
		log.Printf("WARN: Enabling memory profiler endpoint!")
		r.Get("/memprofile", func(w http.ResponseWriter, r *http.Request) {
			f, err := os.Create(config.MemProfileFile)
			if err != nil {
				http.Error(w, fmt.Sprintf("Could not create memory profile: %s", err), http.StatusInternalServerError)
				return
			}
			defer f.Close() // error handling omitted for example
			runtime.GC()    // get up-to-date statistics
			if err := pprof.WriteHeapProfile(f); err != nil {
				http.Error(w, fmt.Sprintf("Could not write memory profile: %s", err), http.StatusInternalServerError)
				return
			}
			fmt.Fprintf(w, "Wrote memory profile")
		})
	}

	log.Fatal(s.ListenAndServe())
}
