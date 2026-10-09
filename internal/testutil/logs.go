package testutil

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"sync"
)

type Logs struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (l *Logs) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buffer.Write(p)
}
func (l *Logs) Contents() string     { l.mu.Lock(); defer l.mu.Unlock(); return l.buffer.String() }
func (l *Logs) Logger() *slog.Logger { return slog.New(slog.NewJSONHandler(l, nil)) }
func (l *Logs) Records() ([]map[string]any, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	d := json.NewDecoder(bytes.NewReader(l.buffer.Bytes()))
	var out []map[string]any
	for d.More() {
		var v map[string]any
		if e := d.Decode(&v); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, nil
}
