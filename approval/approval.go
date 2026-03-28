package approval

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const schemaVersion = 1

var requestIDPattern = regexp.MustCompile(`^req_[0-9a-f]+$`)

type Request struct {
	SchemaVersion int                    `json:"schema_version"`
	RequestID     string                 `json:"request_id"`
	DelegationID  string                 `json:"delegation_id"`
	Timestamp     string                 `json:"timestamp"`
	Action        string                 `json:"action"`
	Description   string                 `json:"description"`
	RiskLevel     string                 `json:"risk_level,omitempty"`
	Context       map[string]interface{} `json:"context,omitempty"`
}

type Response struct {
	SchemaVersion int    `json:"schema_version"`
	RequestID     string `json:"request_id"`
	Decision      string `json:"decision"`
	Reason        string `json:"reason,omitempty"`
	DecidedBy     string `json:"decided_by"`
	Timestamp     string `json:"timestamp"`
}

func NewDelegationID() (string, error) {
	suffix, err := randomHex(8)
	if err != nil {
		return "", err
	}
	return "del_" + suffix, nil
}

func NewRequestID() (string, error) {
	suffix, err := randomHex(8)
	if err != nil {
		return "", err
	}
	return "req_" + suffix, nil
}

func CreateApprovalDir(delegationID string) (string, error) {
	dir := filepath.Join(os.TempDir(), "quancode-approval-"+delegationID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func CleanupApprovalDir(dir string) error {
	if dir == "" {
		return nil
	}
	err := os.RemoveAll(dir)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func WriteRequest(dir string, req Request) error {
	if err := validateRequestID(req.RequestID); err != nil {
		return err
	}
	if req.SchemaVersion == 0 {
		req.SchemaVersion = schemaVersion
	}
	if req.Timestamp == "" {
		req.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}
	return writeJSONAtomic(filepath.Join(dir, "request-"+req.RequestID+".json"), req)
}

func WriteResponse(dir string, resp Response) error {
	if err := validateRequestID(resp.RequestID); err != nil {
		return err
	}
	if resp.SchemaVersion == 0 {
		resp.SchemaVersion = schemaVersion
	}
	if resp.Timestamp == "" {
		resp.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}
	path := filepath.Join(dir, "response-"+resp.RequestID+".json")
	return writeJSONExclusive(path, resp)
}

func ReadRequest(dir, requestID string) (*Request, error) {
	if err := validateRequestID(requestID); err != nil {
		return nil, err
	}
	var req Request
	if err := readJSON(filepath.Join(dir, "request-"+requestID+".json"), &req); err != nil {
		return nil, err
	}
	return &req, nil
}

func ReadResponse(dir, requestID string) (*Response, error) {
	if err := validateRequestID(requestID); err != nil {
		return nil, err
	}
	var resp Response
	if err := readJSON(filepath.Join(dir, "response-"+requestID+".json"), &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func writeJSONAtomic(path string, v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	return os.Rename(tmpPath, path)
}

func readJSON(path string, v interface{}) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func validateRequestID(requestID string) error {
	if !requestIDPattern.MatchString(requestID) {
		return fmt.Errorf("invalid request id %q", requestID)
	}
	return nil
}

func writeJSONExclusive(path string, v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("response already exists for request %s", strings.TrimSuffix(strings.TrimPrefix(filepath.Base(path), "response-"), ".json"))
		}
		return err
	}

	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
