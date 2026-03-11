package loom

import (
	"compress/flate"
	"testing"
)

func TestSetCompressionLevel_Valid(t *testing.T) {
	server, _ := connPair()
	defer server.Close()

	if err := server.SetCompressionLevel(flate.BestSpeed); err != nil {
		t.Fatalf("BestSpeed: %v", err)
	}
	if err := server.SetCompressionLevel(flate.BestCompression); err != nil {
		t.Fatalf("BestCompression: %v", err)
	}
	if err := server.SetCompressionLevel(flate.DefaultCompression); err != nil {
		t.Fatalf("DefaultCompression: %v", err)
	}
}

func TestSetCompressionLevel_Invalid(t *testing.T) {
	server, _ := connPair()
	defer server.Close()

	err := server.SetCompressionLevel(42)
	if err == nil {
		t.Fatal("expected error for invalid compression level")
	}
}

func TestEnableWriteCompression(t *testing.T) {
	server, _ := connPair()
	defer server.Close()

	server.EnableWriteCompression(true)
	if !server.writeCompress {
		t.Fatal("expected writeCompress to be true")
	}

	server.EnableWriteCompression(false)
	if server.writeCompress {
		t.Fatal("expected writeCompress to be false")
	}
}
