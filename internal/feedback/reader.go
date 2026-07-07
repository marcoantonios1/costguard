package feedback

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
)

// Filter selects which records to include when reading the feedback log.
type Filter struct {
	Consumer string // empty = match all
	Role     string // empty = match all
	Model    string // empty = match all
	LastN    int    // 0 = all records; N > 0 = last N records that pass the filter
}

func (f Filter) matches(r FeedbackRecord) bool {
	if f.Consumer != "" && r.Consumer != f.Consumer {
		return false
	}
	if f.Role != "" && r.Role != f.Role {
		return false
	}
	if f.Model != "" && r.Model != f.Model {
		return false
	}
	return true
}

// Reader scans a JSONL feedback log file.
//
// JSONLStore only holds its write mutex for the duration of a single
// fmt.Fprintf syscall, and Reader opens its own file descriptor per Read
// call and reads sequentially. There is no shared state between the two, so
// no locking is needed between concurrent Reader.Read and JSONLStore.Append
// calls.
type Reader struct {
	path string
}

// NewReader builds a Reader over the JSONL file at path.
func NewReader(path string) *Reader {
	return &Reader{path: path}
}

// Read returns all records in the file matching f, silently skipping blank
// or malformed lines. If f.LastN > 0, only the last LastN matching records
// are returned. Read returns (nil, nil) if the file does not exist yet.
func (rd *Reader) Read(ctx context.Context, f Filter) ([]FeedbackRecord, error) {
	file, err := os.OpenFile(rd.path, os.O_RDONLY, 0)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()

	var matched []FeedbackRecord
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec FeedbackRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		if f.matches(rec) {
			matched = append(matched, rec)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	if f.LastN > 0 && len(matched) > f.LastN {
		matched = matched[len(matched)-f.LastN:]
	}

	return matched, nil
}
