package indexer

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestChunkText_Empty(t *testing.T) {
	assert.Nil(t, ChunkText("", 500))
	assert.Nil(t, ChunkText("   ", 500))
}

func TestChunkText_SingleSmallParagraph(t *testing.T) {
	chunks := ChunkText("Hello world", 500)
	assert.Equal(t, []string{"Hello world"}, chunks)
}

func TestChunkText_MultipleParagraphsCombined(t *testing.T) {
	text := "First paragraph.\n\nSecond paragraph.\n\nThird paragraph."
	chunks := ChunkText(text, 500)
	assert.Len(t, chunks, 1)
	assert.Contains(t, chunks[0], "First paragraph.")
	assert.Contains(t, chunks[0], "Third paragraph.")
}

func TestChunkText_SplitOnParagraphBoundary(t *testing.T) {
	// Create two paragraphs each ~300 bytes
	para := strings.Repeat("abcde ", 50) // 300 bytes
	para = strings.TrimSpace(para)
	text := para + "\n\n" + para

	chunks := ChunkText(text, 500)
	assert.Len(t, chunks, 2)
}

func TestChunkText_LargeParagraphSplit(t *testing.T) {
	// Create a single paragraph of 1200 bytes (240 words * 5 bytes each)
	words := make([]string, 240)
	for i := range words {
		words[i] = "word" // 4 bytes + 1 space = 5 per word
	}
	text := strings.Join(words, " ") // 240*4 + 239 = 1199 bytes

	chunks := ChunkText(text, 500)
	assert.Len(t, chunks, 3)
	assert.LessOrEqual(t, len(chunks[0]), 500)
	assert.LessOrEqual(t, len(chunks[1]), 500)
	assert.Greater(t, len(chunks[2]), 0)
}

func TestChunkText_DefaultMaxBytes(t *testing.T) {
	chunks := ChunkText("hello", 0)
	assert.Equal(t, []string{"hello"}, chunks)
}

func TestChunkText_EmptyParagraphsIgnored(t *testing.T) {
	text := "First.\n\n\n\n\n\nSecond."
	chunks := ChunkText(text, 500)
	assert.Len(t, chunks, 1)
	assert.Equal(t, "First.\n\nSecond.", chunks[0])
}

func TestChunkText_ParagraphKeptWhole(t *testing.T) {
	// A paragraph under the limit should not be split
	para := strings.Repeat("x", 400)
	chunks := ChunkText(para, 500)
	assert.Len(t, chunks, 1)
	assert.Equal(t, para, chunks[0])
}

func TestChunkText_JoinerAccountedInLimit(t *testing.T) {
	// Two paragraphs that fit individually but exceed the limit when joined with "\n\n"
	p1 := strings.Repeat("a", 250)
	p2 := strings.Repeat("b", 250)
	text := p1 + "\n\n" + p2

	chunks := ChunkText(text, 500)
	// 250 + 2 + 250 = 502 > 500, so they should be split
	assert.Len(t, chunks, 2)
	assert.Equal(t, p1, chunks[0])
	assert.Equal(t, p2, chunks[1])
}
