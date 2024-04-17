package quickfile

import (
	"database/sql"
	"os"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Duration time.Duration

func (d *Duration) UnmarshalText(b []byte) error {
	x, err := time.ParseDuration(string(b))
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
	Timeout            Duration
	Datapath           string                    // Place to put the files
	Port               int                       // The port obviously
	TotalUploadLimit   int64                     // Max size for the totality of file uploads (not size of db!!)
	DefaultUploadLimit int64                     // Size in bytes of account upload
	DefaultFileLimit   int                       // Default Amount of files per account
	UploadSizeLimit    int                       // Individual file upload limit
	SimpleFormLimit    int                       // Size that a simple form can be (not file uploads)
	HeaderLimit        int                       // max size of the http header
	MaxFileTags        int                       // Maximum amount of tags on a single file
	ResultsPerPage     int                       // Amount of files to show per page
	RateLimitInterval  Duration                  // span of time for rate limiting
	RateLimitCount     int                       // Amount of times a user from a single IP can access per interval
	DefaultMinExpire   Duration                  // Min expire measured in minutes
	DefaultExpire      Duration                  // Default expiration value if none is set
	DefaultMaxExpire   Duration                  // Maximum allowed expiration
	CacheTime          Duration                  // How long to cache
	Accounts           map[string]*AccountConfig // The accounts usable
	AllowedMimeTypes   []string                  // If set, only allow mimetypes from this list
}

func GetDefaultConfig() Config {
	return Config{
		Accounts:           make(map[string]*AccountConfig),
		Timeout:            Duration(120 * time.Second),
		Port:               5007,
		TotalUploadLimit:   1_000_000_000, // 1gb
		DefaultUploadLimit: 100_000_000,
		DefaultFileLimit:   1000,
		UploadSizeLimit:    100_000_000,
		MaxFileTags:        10,
		ResultsPerPage:     100,
		SimpleFormLimit:    100_000,
		HeaderLimit:        100_000,
		Datapath:           "uploads.db",
		RateLimitCount:     100,
		RateLimitInterval:  Duration(1 * time.Minute),
		DefaultMinExpire:   Duration(5 * time.Minute),
		DefaultMaxExpire:   Duration(72 * time.Hour),
		DefaultExpire:      Duration(24 * time.Hour),
		CacheTime:          Duration(365 * 24 * time.Hour),
	}
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
	return sql.Open("sqlite3", c.Datapath)
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
