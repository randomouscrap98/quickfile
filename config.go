package main

type AccountConfig struct {
	UploadLimit int
	FileLimit   int
}

type Config struct {
	Timeout            float64                   // Seconds to timeout
	Datapath           string                    // Place to put the files
	Port               int                       // The port obviously
	DefaultUploadLimit int                       // Size in bytes of account upload
	DefaultFileLimit   int                       // Default Amount of files per account
	UploadSizeLimit    int                       // Individual file upload limit
	Accounts           map[string]*AccountConfig // The accounts usable
}

func GetDefaultConfig() Config {
	return Config{
		Timeout:            60,
		Port:               5007,
		DefaultUploadLimit: 100_000_000,
		DefaultFileLimit:   1000,
		UploadSizeLimit:    100_000_000,
		Datapath:           "uploads.db",
	}
}

// Apply the defaults to all the accounts so you can directly use the values
func (c *Config) ApplyDefaults() {
	for _, v := range c.Accounts {
		if v.UploadLimit == 0 {
			v.UploadLimit = c.DefaultUploadLimit
		}
		if v.FileLimit == 0 {
			v.FileLimit = c.DefaultFileLimit
		}
	}
}
