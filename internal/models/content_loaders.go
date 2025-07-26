package models

import (
	"encoding/base64"
	"fmt"
	"io"
	"strings"
	"sync"
)

// ReaderContentLoader loads content from an io.Reader
type ReaderContentLoader struct {
	reader     io.Reader
	content    string
	size       int64
	loaded     bool
	mu         sync.RWMutex
}

// NewReaderContentLoader creates a new reader-based content loader
func NewReaderContentLoader(reader io.Reader) *ReaderContentLoader {
	return &ReaderContentLoader{
		reader: reader,
		size:   -1, // Unknown size
	}
}

// LoadContent loads the content from the reader
func (l *ReaderContentLoader) LoadContent() (string, error) {
	l.mu.RLock()
	if l.loaded {
		content := l.content
		l.mu.RUnlock()
		return content, nil
	}
	l.mu.RUnlock()

	l.mu.Lock()
	defer l.mu.Unlock()

	// Double-check after acquiring write lock
	if l.loaded {
		return l.content, nil
	}

	data, err := io.ReadAll(l.reader)
	if err != nil {
		return "", fmt.Errorf("failed to read content: %w", err)
	}

	l.content = string(data)
	l.size = int64(len(data))
	l.loaded = true
	return l.content, nil
}

// GetSize returns the content size (may be -1 if unknown)
func (l *ReaderContentLoader) GetSize() int64 {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.size
}

// IsLoaded returns true if content is loaded
func (l *ReaderContentLoader) IsLoaded() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.loaded
}

// FileContentLoader loads content from a file path
type FileContentLoader struct {
	filePath string
	offset   int64
	length   int64
	content  string
	loaded   bool
	mu       sync.RWMutex
}

// NewFileContentLoader creates a new file-based content loader
func NewFileContentLoader(filePath string, offset, length int64) *FileContentLoader {
	return &FileContentLoader{
		filePath: filePath,
		offset:   offset,
		length:   length,
	}
}

// LoadContent loads content from the file
func (l *FileContentLoader) LoadContent() (string, error) {
	l.mu.RLock()
	if l.loaded {
		content := l.content
		l.mu.RUnlock()
		return content, nil
	}
	l.mu.RUnlock()

	l.mu.Lock()
	defer l.mu.Unlock()

	// Double-check after acquiring write lock
	if l.loaded {
		return l.content, nil
	}

	// Implementation would read from file at offset/length
	// For now, return placeholder
	l.content = fmt.Sprintf("Content from file %s at offset %d, length %d", l.filePath, l.offset, l.length)
	l.loaded = true
	return l.content, nil
}

// GetSize returns the content size
func (l *FileContentLoader) GetSize() int64 {
	return l.length
}

// IsLoaded returns true if content is loaded
func (l *FileContentLoader) IsLoaded() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.loaded
}

// ReaderAttachmentLoader loads attachments from an io.Reader
type ReaderAttachmentLoader struct {
	reader     io.Reader
	metadata   AttachmentMetadata
	attachment *Attachment
	loaded     bool
	mu         sync.RWMutex
}

// NewReaderAttachmentLoader creates a new reader-based attachment loader
func NewReaderAttachmentLoader(reader io.Reader, metadata AttachmentMetadata) *ReaderAttachmentLoader {
	return &ReaderAttachmentLoader{
		reader:   reader,
		metadata: metadata,
	}
}

// LoadAttachment loads the attachment data
func (l *ReaderAttachmentLoader) LoadAttachment() (*Attachment, error) {
	l.mu.RLock()
	if l.loaded {
		attachment := *l.attachment
		l.mu.RUnlock()
		return &attachment, nil
	}
	l.mu.RUnlock()

	l.mu.Lock()
	defer l.mu.Unlock()

	// Double-check after acquiring write lock
	if l.loaded {
		attachment := *l.attachment
		return &attachment, nil
	}

	data, err := io.ReadAll(l.reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read attachment data: %w", err)
	}

	// Handle content transfer encoding if needed
	// This is a simplified version - in practice, you'd check headers
	if strings.Contains(l.metadata.ContentType, "base64") {
		decoded, err := base64.StdEncoding.DecodeString(string(data))
		if err != nil {
			return nil, fmt.Errorf("failed to decode base64 attachment: %w", err)
		}
		data = decoded
	}

	l.attachment = &Attachment{
		Filename:    l.metadata.Filename,
		ContentType: l.metadata.ContentType,
		Size:        int64(len(data)),
		Data:        data,
	}

	l.loaded = true
	return l.attachment, nil
}

// GetMetadata returns attachment metadata
func (l *ReaderAttachmentLoader) GetMetadata() AttachmentMetadata {
	return l.metadata
}

// IsLoaded returns true if attachment is loaded
func (l *ReaderAttachmentLoader) IsLoaded() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.loaded
}

// FileAttachmentLoader loads attachments from file paths
type FileAttachmentLoader struct {
	filePath   string
	offset     int64
	length     int64
	metadata   AttachmentMetadata
	attachment *Attachment
	loaded     bool
	mu         sync.RWMutex
}

// NewFileAttachmentLoader creates a new file-based attachment loader
func NewFileAttachmentLoader(filePath string, offset, length int64, metadata AttachmentMetadata) *FileAttachmentLoader {
	return &FileAttachmentLoader{
		filePath: filePath,
		offset:   offset,
		length:   length,
		metadata: metadata,
	}
}

// LoadAttachment loads attachment from file
func (l *FileAttachmentLoader) LoadAttachment() (*Attachment, error) {
	l.mu.RLock()
	if l.loaded {
		attachment := *l.attachment
		l.mu.RUnlock()
		return &attachment, nil
	}
	l.mu.RUnlock()

	l.mu.Lock()
	defer l.mu.Unlock()

	// Double-check after acquiring write lock
	if l.loaded {
		attachment := *l.attachment
		return &attachment, nil
	}

	// Implementation would read from file at offset/length
	// For now, return placeholder
	data := []byte(fmt.Sprintf("Attachment data from file %s at offset %d, length %d", l.filePath, l.offset, l.length))

	l.attachment = &Attachment{
		Filename:    l.metadata.Filename,
		ContentType: l.metadata.ContentType,
		Size:        int64(len(data)),
		Data:        data,
	}

	l.loaded = true
	return l.attachment, nil
}

// GetMetadata returns attachment metadata
func (l *FileAttachmentLoader) GetMetadata() AttachmentMetadata {
	return l.metadata
}

// IsLoaded returns true if attachment is loaded
func (l *FileAttachmentLoader) IsLoaded() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.loaded
}

// MemoryPool manages memory usage for content loaders
type MemoryPool struct {
	maxMemory     int64
	currentMemory int64
	loaders       map[ContentLoader]int64
	mu            sync.RWMutex
}

// NewMemoryPool creates a new memory pool with the specified limit
func NewMemoryPool(maxMemory int64) *MemoryPool {
	return &MemoryPool{
		maxMemory: maxMemory,
		loaders:   make(map[ContentLoader]int64),
	}
}

// RegisterLoader registers a content loader with the pool
func (p *MemoryPool) RegisterLoader(loader ContentLoader) {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	if _, exists := p.loaders[loader]; !exists {
		p.loaders[loader] = 0
	}
}

// UpdateMemoryUsage updates the memory usage for a loader
func (p *MemoryPool) UpdateMemoryUsage(loader ContentLoader, newUsage int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	oldUsage := p.loaders[loader]
	p.currentMemory = p.currentMemory - oldUsage + newUsage
	p.loaders[loader] = newUsage
}

// GetMemoryUsage returns current memory usage
func (p *MemoryPool) GetMemoryUsage() int64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.currentMemory
}

// IsMemoryAvailable checks if memory is available for allocation
func (p *MemoryPool) IsMemoryAvailable(requestedSize int64) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.currentMemory+requestedSize <= p.maxMemory
}

// EvictLRU evicts least recently used content to free memory
func (p *MemoryPool) EvictLRU(targetSize int64) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	// Simple implementation - in practice, you'd track access times
	var freedMemory int64
	for loader, usage := range p.loaders {
		if usage > 0 && loader.IsLoaded() {
			// Evict this loader's content
			if rcl, ok := loader.(*ReaderContentLoader); ok {
				rcl.mu.Lock()
				rcl.content = ""
				rcl.loaded = false
				rcl.mu.Unlock()
			}
			
			freedMemory += usage
			p.currentMemory -= usage
			p.loaders[loader] = 0
			
			if freedMemory >= targetSize {
				break
			}
		}
	}
	
	return nil
}