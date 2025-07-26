package models

import (
	"strings"
	"testing"
)

func TestStreamingEmailBasic(t *testing.T) {
	rawEmail := `From: test@example.com
To: recipient@example.com
Subject: Test Email
Date: Mon, 01 Jan 2024 12:00:00 +0000
Message-ID: <test@example.com>
Content-Type: text/plain

This is a test email body.`

	parser := NewStreamingEmailParser()
	reader := strings.NewReader(rawEmail)
	streamingEmail, err := parser.ParseStreamingEmail(reader)
	if err != nil {
		t.Fatalf("Failed to parse streaming email: %v", err)
	}

	// Test metadata is available
	if streamingEmail.Subject != "Test Email" {
		t.Errorf("Expected subject 'Test Email', got '%s'", streamingEmail.Subject)
	}

	if streamingEmail.From.Email != "test@example.com" {
		t.Errorf("Expected from 'test@example.com', got '%s'", streamingEmail.From.Email)
	}

	// Test body is not loaded initially
	if streamingEmail.IsBodyLoaded() {
		t.Error("Body should not be loaded initially")
	}

	// Test lazy loading of body
	body, err := streamingEmail.GetBody()
	if err != nil {
		t.Fatalf("Failed to load body: %v", err)
	}

	if !strings.Contains(body, "This is a test email body") {
		t.Errorf("Body content incorrect: %s", body)
	}

	// Test body is now loaded
	if !streamingEmail.IsBodyLoaded() {
		t.Error("Body should be loaded after GetBody()")
	}
}

func TestContentLoader(t *testing.T) {
	content := "This is test content"
	reader := strings.NewReader(content)
	loader := NewReaderContentLoader(reader)

	// Test initial state
	if loader.IsLoaded() {
		t.Error("Content should not be loaded initially")
	}

	// Test loading
	loadedContent, err := loader.LoadContent()
	if err != nil {
		t.Fatalf("Failed to load content: %v", err)
	}

	if loadedContent != content {
		t.Errorf("Expected '%s', got '%s'", content, loadedContent)
	}

	// Test loaded state
	if !loader.IsLoaded() {
		t.Error("Content should be loaded after LoadContent()")
	}

	// Test size
	if loader.GetSize() != int64(len(content)) {
		t.Errorf("Expected size %d, got %d", len(content), loader.GetSize())
	}
}

func TestMemoryPool(t *testing.T) {
	pool := NewMemoryPool(1024) // 1KB limit

	content := strings.Repeat("A", 512) // 512 bytes
	reader := strings.NewReader(content)
	loader := NewReaderContentLoader(reader)

	pool.RegisterLoader(loader)

	// Load content
	_, err := loader.LoadContent()
	if err != nil {
		t.Fatalf("Failed to load content: %v", err)
	}

	pool.UpdateMemoryUsage(loader, 512)

	// Test memory usage
	if pool.GetMemoryUsage() != 512 {
		t.Errorf("Expected memory usage 512, got %d", pool.GetMemoryUsage())
	}

	// Test memory availability
	if !pool.IsMemoryAvailable(512) {
		t.Error("Should have memory available for 512 bytes")
	}

	if pool.IsMemoryAvailable(1024) {
		t.Error("Should not have memory available for 1024 bytes")
	}
}

func BenchmarkStreamingEmailParsing(b *testing.B) {
	rawEmail := `From: test@example.com
To: recipient@example.com
Subject: Test Email
Date: Mon, 01 Jan 2024 12:00:00 +0000
Message-ID: <test@example.com>
Content-Type: text/plain

` + strings.Repeat("This is a test email body. ", 100)

	parser := NewStreamingEmailParser()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		reader := strings.NewReader(rawEmail)
		_, err := parser.ParseStreamingEmail(reader)
		if err != nil {
			b.Fatalf("Failed to parse email: %v", err)
		}
	}
}

func BenchmarkContentLoading(b *testing.B) {
	content := strings.Repeat("A", 10240) // 10KB content

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		reader := strings.NewReader(content)
		loader := NewReaderContentLoader(reader)

		_, err := loader.LoadContent()
		if err != nil {
			b.Fatalf("Failed to load content: %v", err)
		}
	}
}

func BenchmarkStreamingVsRegular(b *testing.B) {
	rawEmail := `From: test@example.com
To: recipient@example.com
Subject: Test Email
Date: Mon, 01 Jan 2024 12:00:00 +0000
Message-ID: <test@example.com>
Content-Type: text/plain

` + strings.Repeat("This is a test email body. ", 1000)

	b.Run("Regular", func(b *testing.B) {
		parser := NewEmailParser()

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_, err := parser.ParseRawEmail(rawEmail)
			if err != nil {
				b.Fatalf("Failed to parse email: %v", err)
			}
		}
	})

	b.Run("Streaming", func(b *testing.B) {
		parser := NewStreamingEmailParser()

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			reader := strings.NewReader(rawEmail)
			_, err := parser.ParseStreamingEmail(reader)
			if err != nil {
				b.Fatalf("Failed to parse email: %v", err)
			}
		}
	})
}