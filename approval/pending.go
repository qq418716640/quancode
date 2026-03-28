package approval

import (
	"os"
	"path/filepath"
	"strings"
)

func PendingRequests(dir string) ([]*Request, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	responded := make(map[string]bool)
	var requestIDs []string
	for _, entry := range entries {
		name := entry.Name()
		switch {
		case strings.HasPrefix(name, "response-") && strings.HasSuffix(name, ".json"):
			requestID := strings.TrimSuffix(strings.TrimPrefix(name, "response-"), ".json")
			responded[requestID] = true
		case strings.HasPrefix(name, "request-") && strings.HasSuffix(name, ".json"):
			requestIDs = append(requestIDs, strings.TrimSuffix(strings.TrimPrefix(name, "request-"), ".json"))
		}
	}

	var pending []*Request
	for _, requestID := range requestIDs {
		if responded[requestID] {
			continue
		}
		req, err := ReadRequest(dir, requestID)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		pending = append(pending, req)
	}
	return pending, nil
}

func ResponseExists(dir, requestID string) (bool, error) {
	if err := validateRequestID(requestID); err != nil {
		return false, err
	}
	_, err := os.Stat(filepath.Join(dir, "response-"+requestID+".json"))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}
