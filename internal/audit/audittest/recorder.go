// Package audittest captures structured audit records for integration tests.
package audittest

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"sync"
)

type Recorder struct {
	mu   sync.Mutex
	data bytes.Buffer
}

func (r *Recorder) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.data.Write(p)
}
func (r *Recorder) Logger() *slog.Logger { return slog.New(slog.NewJSONHandler(r, nil)) }
func (r *Recorder) Records() []map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	decoder := json.NewDecoder(bytes.NewReader(r.data.Bytes()))
	var records []map[string]any
	for {
		var record map[string]any
		err := decoder.Decode(&record)
		if err == io.EOF {
			break
		}
		if err != nil {
			panic(err)
		}
		if record["audit"] == true {
			records = append(records, record)
		}
	}
	return records
}
func (r *Recorder) Events(name string) []map[string]any {
	var found []map[string]any
	for _, record := range r.Records() {
		if record["event"] == name {
			found = append(found, record)
		}
	}
	return found
}
