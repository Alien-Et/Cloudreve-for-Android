package indexer

import "strings"

const defaultMaxBytes = 2000

// ChunkText splits text into chunks of approximately maxBytes bytes each.
// It splits on paragraph breaks (\n\n), combines small paragraphs until the
// byte limit is reached, and splits large paragraphs at word boundaries.
func ChunkText(text string, maxBytes int) []string {
	if maxBytes <= 0 {
		maxBytes = defaultMaxBytes
	}

	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	paragraphs := strings.Split(text, "\n\n")
	var chunks []string
	var current []string
	currentBytes := 0

	for _, para := range paragraphs {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}

		paraBytes := len(para)

		// If a single paragraph exceeds maxBytes, split it at word boundaries
		if paraBytes > maxBytes {
			// Flush accumulated content first
			if currentBytes > 0 {
				chunks = append(chunks, strings.Join(current, "\n\n"))
				current = nil
				currentBytes = 0
			}
			chunks = append(chunks, splitByBytes(para, maxBytes)...)
			continue
		}

		// If adding this paragraph (plus separator) would exceed the limit, flush
		joinerLen := 0
		if currentBytes > 0 {
			joinerLen = 2 // "\n\n"
		}
		if currentBytes+joinerLen+paraBytes > maxBytes && currentBytes > 0 {
			chunks = append(chunks, strings.Join(current, "\n\n"))
			current = nil
			currentBytes = 0
		}

		if currentBytes > 0 {
			currentBytes += 2 // account for "\n\n" joiner
		}
		current = append(current, para)
		currentBytes += paraBytes
	}

	// Flush remaining
	if currentBytes > 0 {
		chunks = append(chunks, strings.Join(current, "\n\n"))
	}

	return chunks
}

// splitByBytes splits text into chunks at word boundaries, each at most maxBytes bytes.
func splitByBytes(text string, maxBytes int) []string {
	words := strings.Fields(text)
	var chunks []string
	var current []string
	currentBytes := 0

	for _, w := range words {
		wLen := len(w)
		spaceLen := 0
		if currentBytes > 0 {
			spaceLen = 1
		}

		if currentBytes+spaceLen+wLen > maxBytes && currentBytes > 0 {
			chunks = append(chunks, strings.Join(current, " "))
			current = nil
			currentBytes = 0
			spaceLen = 0
		}

		current = append(current, w)
		currentBytes += spaceLen + wLen
	}

	if len(current) > 0 {
		chunks = append(chunks, strings.Join(current, " "))
	}

	return chunks
}
