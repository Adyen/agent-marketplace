package frozengzip

import (
	"bytes"
	"compress/gzip"
	"encoding/hex"
	"io"
	"testing"
)

// TestBestSpeedEmitsCanonicalHeader pins the exact 10-byte RFC 1952 header of
// the canonical encoding: the bytes a marketplace server's artifact carries
// before its deflate stream.
func TestBestSpeedEmitsCanonicalHeader(t *testing.T) {
	var buf bytes.Buffer
	w := NewBestSpeed(&buf)
	if _, err := w.Write([]byte("payload")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if got, want := hex.EncodeToString(buf.Bytes()[:10]), "1f8b08000000000004ff"; got != want {
		t.Fatalf("header = %s, want %s", got, want)
	}
}

// TestBestSpeedRoundTripsThroughStdlibGzip verifies the frozen encoder's
// output remains a valid gzip stream decodable by the standard library.
func TestBestSpeedRoundTripsThroughStdlibGzip(t *testing.T) {
	data := bytes.Repeat([]byte("canonical artifact payload with repeated runs of text "), 100)

	var buf bytes.Buffer
	w := NewBestSpeed(&buf)
	if _, err := w.Write(data); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	r, err := gzip.NewReader(&buf)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read all: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("round-trip mismatch: got %d bytes, want %d", len(got), len(data))
	}
}

// TestBestSpeedCloseWithoutWrite emits a complete empty gzip stream, matching
// compress/gzip's behavior for a writer closed without data.
func TestBestSpeedCloseWithoutWrite(t *testing.T) {
	var buf bytes.Buffer
	w := NewBestSpeed(&buf)
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}

	r, err := gzip.NewReader(&buf)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read all: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("empty stream decoded %d bytes, want 0", len(got))
	}
}

// TestBestSpeedDeterministicAcrossInstances guards the determinism contract:
// the same input must always produce the same bytes.
func TestBestSpeedDeterministicAcrossInstances(t *testing.T) {
	data := []byte("the same input must always compress to the same output bytes")

	var first []byte
	for i := 0; i < 3; i++ {
		var buf bytes.Buffer
		w := NewBestSpeed(&buf)
		if _, err := w.Write(data); err != nil {
			t.Fatalf("write: %v", err)
		}
		if err := w.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
		if i == 0 {
			first = buf.Bytes()
			continue
		}
		if !bytes.Equal(first, buf.Bytes()) {
			t.Fatalf("encoding %d differs from first encoding", i)
		}
	}
}
