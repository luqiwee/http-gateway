package log

import (
	"io"
	"sync/atomic"
	"time"

	"go.uber.org/zap/zapcore"
)

const defaultAsyncBufferSize = 65536

type asyncWriteSyncer struct {
	out     zapcore.WriteSyncer
	entries chan []byte
	flush   chan chan struct{}
	dropped atomic.Uint64
}

func newAsyncWriteSyncer(out io.Writer, bufferSize int) *asyncWriteSyncer {
	if bufferSize <= 0 {
		bufferSize = defaultAsyncBufferSize
	}
	writer := &asyncWriteSyncer{
		out:     zapcore.AddSync(out),
		entries: make(chan []byte, bufferSize),
		flush:   make(chan chan struct{}),
	}
	go writer.run()
	return writer
}

func (w *asyncWriteSyncer) Write(p []byte) (int, error) {
	entry := make([]byte, len(p))
	copy(entry, p)

	select {
	case w.entries <- entry:
	default:
		w.dropped.Add(1)
	}
	return len(p), nil
}

func (w *asyncWriteSyncer) Sync() error {
	done := make(chan struct{})
	select {
	case w.flush <- done:
	case <-time.After(2 * time.Second):
		return nil
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
	return nil
}

func (w *asyncWriteSyncer) Dropped() uint64 {
	return w.dropped.Load()
}

func (w *asyncWriteSyncer) run() {
	for {
		select {
		case entry := <-w.entries:
			_, _ = w.out.Write(entry)
		case done := <-w.flush:
			w.drain()
			_ = w.out.Sync()
			close(done)
		}
	}
}

func (w *asyncWriteSyncer) drain() {
	for {
		select {
		case entry := <-w.entries:
			_, _ = w.out.Write(entry)
		default:
			return
		}
	}
}
