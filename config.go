package quickfile

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const (
	ForeverDuration = "2000000h"
	BusyTimeout     = 5000
)

type Duration time.Duration

func (d *Duration) UnmarshalText(b []byte) error {
	bs := string(b)
	if bs == "never" || bs == "infinite" {
		bs = ForeverDuration
	}
	x, err := time.ParseDuration(bs)
	if err != nil {
		return err
	}
	*d = Duration(x)
	return nil
}

type AccountConfig struct {
	UploadLimit int64
	FileLimit   int
	MinExpire   Duration
	MaxExpire   Duration
}

type Config struct {
	Timeout             Duration
	Datapath            string                    // Place to put the files
	CookieName          string                    // Name of authentication cookie
	Port                int                       // The port obviously
	MemProfileFile      string                    // If set, determines where to store mem profile when endpoint called. Endpoint disabled if empty
	TotalUploadLimit    int64                     // Max size for the totality of file uploads (not size of db!!)
	DefaultUploadLimit  int64                     // Size in bytes of account upload
	DefaultFileLimit    int                       // Default Amount of files per account
	UploadSizeLimit     int                       // Individual file upload limit
	SimpleFormLimit     int                       // Size that a simple form can be (not file uploads)
	HeaderLimit         int                       // max size of the http header
	MaxFileTags         int                       // Maximum amount of tags on a single file
	MaxFileName         int                       // Max length of filename. Files will be rejected if larger than this
	ResultsPerPage      int                       // Amount of files to show per page
	VacuumThreshold     int64                     // Amount of bytes required before vacuum. Set to 0 to disable
	MaintenanceInterval Duration                  // Interval between maintenance cycles (should be less than the min expire)
	RateLimitInterval   Duration                  // span of time for rate limiting
	RateLimitCount      int                       // Amount of times a user from a single IP can access per interval
	DefaultMinExpire    Duration                  // Min expire measured in minutes
	DefaultExpire       Duration                  // Default expiration value if none is set
	DefaultMaxExpire    Duration                  // Maximum allowed expiration
	CacheTime           Duration                  // How long to cache
	Accounts            map[string]*AccountConfig // The accounts usable
	MimeTypeRedirect    map[string]string         // Make certain mime types other mime types
	AllowedMimeTypes    []string                  // If set, only allow mimetypes from this list
	ForbiddenMimeTypes  []string                  // All mimes in this list are blocked
}

func GetDefaultConfig_Toml() string {
	randomUser := make([]byte, 16)
	_, err := rand.Read(randomUser)
	if err != nil {
		log.Printf("WARN: couldn't generate random user")
	}
	randomHex := hex.EncodeToString(randomUser)
	return fmt.Sprintf(`# Config auto-generated on %s
Datapath="uploads.db"   # Where to store the upload database (one file)
Timeout="2m"            # Timeout for requests (upload/download). Format is like 1h2m3s etc
Port=5007               # Which port to run the server on
RateLimitCount=100      # Requests allowed per interval
RateLimitInterval="1m"  # Requests limiting interval (rate limiting with RateLimitCount)
CacheTime="8760h"       # The max-age cache time (how long you want the browser to cache files)
CookieName="quickile_account"   # The name of the cookie
TotalUploadLimit=1_000_000_000  # 1GB, total file database max
DefaultUploadLimit=100_000_000  # The default upload limit for accounts
DefaultFileLimit=100            # The default limit of files per user
DefaultMinExpire="5m"           # The default min expire for files on an account
DefaultMaxExpire="72h"          # The default max expire for files on an account
DefaultExpire="24h"             # The default value to put in the expire input on the page
UploadSizeLimit=100_000_000     # Maximum individual file size
MaxFileTags=10                  # Maximum tags per file
MaxFileName=128                 # Max length of filename
ResultsPerPage=100              # Amount of files to list per page
SimpleFormLimit=100_000         # Size limit for simple forms (you usually don't need to change this)
HeaderLimit=100_000             # Size limit for http header (you usually don't need to change this)
# How much "empty space" to leave before vacuuming. 0 means no vacuuming. This is a delicate
# balance: vacuuming defragments/compacts the database. When files are deleted or expired,
# the space is not immediately reclaimed. The space will be reused by new files, so it is
# not necessary to vacuum, but letting the database go too long without vacuuming can make
# it run slower. But, vacuuming too often will greatly increase disk writes, and can 
# legitimately decrease the life of drives, especially when the database is large (gigabytes).
# For safety, and because it is not required, I turn off vacuuming. When it is set, the 
# database will vacuum when the "unused space" reaches the limit you set. A good amount
# might be something like 100_000_000
VacuumThreshold=0
MaintenanceInterval="10m"

# Some mime types are either dangerous (html) and some are like... unknown (empty string).
# If you want other mime redirects, add them
[MimeTypeRedirect]
""="application/octet-stream"
"text/html"="text/plain"

# This is how you define an account. The fields are optional:
# if not defined, it will use the defaults defined above
[Accounts.%s]
# MinExpire="1m"
# MaxExpire="never"
# UploadLimit=1_000_000_000
# FileLimit=10_000
`, time.Now().Format(time.RFC3339), randomHex)
}

// Apply the defaults to all the accounts so you can directly use the values
func (c *Config) ApplyDefaults() {
	for k, v := range c.Accounts {
		if v == nil {
			v = &AccountConfig{}
			c.Accounts[k] = v
		}
		v.UploadLimit = max(v.UploadLimit, c.DefaultUploadLimit)
		v.FileLimit = max(v.FileLimit, c.DefaultFileLimit)
		v.MinExpire = max(v.MinExpire, c.DefaultMinExpire)
		v.MaxExpire = max(v.MaxExpire, c.DefaultMaxExpire)
	}
}

func (c *Config) OpenDb() (*sql.DB, error) {
	return sql.Open("sqlite3", fmt.Sprintf("%s?_busy_timeout=%d", c.Datapath, BusyTimeout))
}

func (c *Config) DbSize() (int64, error) {
	file, err := os.Open(c.Datapath)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	fileInfo, err := file.Stat()
	if err != nil {
		return 0, err
	}
	return fileInfo.Size(), nil
}
