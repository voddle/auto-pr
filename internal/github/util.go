package github

import (
	"bytes"
	"io"
)

// jsonReader wraps bytes that may contain concatenated JSON arrays
// (from gh --paginate) into a single valid JSON reader.
func jsonReader(data []byte) io.Reader {
	return bytes.NewReader(data)
}
