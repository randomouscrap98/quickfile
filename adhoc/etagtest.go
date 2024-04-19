package main

import (
	"math/rand"
	"net/http"
	"time"
	"bytes"
)

var data []byte

func main() {
	data = make([]byte, 4*1024*1024)
	rand.Read(data)
	modtime := time.Now()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Etag", `"some_unique_string"`)
		http.ServeContent(w, r, "data.bin", modtime, bytes.NewReader(data))
	})

	http.ListenAndServe(":8080", nil)
}
