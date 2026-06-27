package log

import (
	"bytes"
	"strings"
	"testing"
)

func TestAsyncWriteSyncerFlushesEntries(t *testing.T) {
	var buf bytes.Buffer
	writer := newAsyncWriteSyncer(&buf, 8)

	if _, err := writer.Write([]byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Sync(); err != nil {
		t.Fatal(err)
	}

	if got := buf.String(); !strings.Contains(got, "hello") {
		t.Fatalf("buffer = %q, want hello", got)
	}
}
