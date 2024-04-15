package main

import (
	"time"
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
	MaxFileTags        int                       // Maximum amount of tags on a single file
	DefaultMinExpire   Duration                  // Min expire measured in minutes
	DefaultExpire      Duration                  // Default expiration value if none is set
	DefaultMaxExpire   Duration                  // Maximum allowed expiration
	Accounts           map[string]*AccountConfig // The accounts usable
	AllowedMimeTypes   []string                  // If set, only allow mimetypes from this list
}

func GetDefaultConfig() Config {
	return Config{
		Timeout:            Duration(60 * time.Second),
		Port:               5007,
		TotalUploadLimit:   1_000_000_000, // 1gb
		DefaultUploadLimit: 100_000_000,
		DefaultFileLimit:   1000,
		UploadSizeLimit:    100_000_000,
		MaxFileTags:        10,
		Datapath:           "uploads.db",
		DefaultMinExpire:   Duration(5 * time.Minute),
		DefaultMaxExpire:   Duration(72 * time.Hour),
		DefaultExpire:      Duration(24 * time.Hour),
	}
}

// Apply the defaults to all the accounts so you can directly use the values
func (c *Config) ApplyDefaults() {
	for _, v := range c.Accounts {
		v.UploadLimit = max(v.UploadLimit, c.DefaultUploadLimit)
		v.FileLimit = max(v.FileLimit, c.DefaultFileLimit)
		v.MinExpire = max(v.MinExpire, c.DefaultMinExpire)
		v.MaxExpire = max(v.MaxExpire, c.DefaultMaxExpire)
	}
}
