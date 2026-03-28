package approval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewIDsHaveExpectedPrefixes(t *testing.T) {
	gotDelegationID, err := NewDelegationID()
	if err != nil {
		t.Fatalf("NewDelegationID: %v", err)
	}
	if !strings.HasPrefix(gotDelegationID, "del_") {
		t.Fatalf("expected del_ prefix, got %q", gotDelegationID)
	}
	gotRequestID, err := NewRequestID()
	if err != nil {
		t.Fatalf("NewRequestID: %v", err)
	}
	if !strings.HasPrefix(gotRequestID, "req_") {
		t.Fatalf("expected req_ prefix, got %q", gotRequestID)
	}
}

func TestCreateAndCleanupApprovalDir(t *testing.T) {
	delegationID, err := NewDelegationID()
	if err != nil {
		t.Fatalf("NewDelegationID: %v", err)
	}
	dir, err := CreateApprovalDir(delegationID)
	if err != nil {
		t.Fatalf("CreateApprovalDir: %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("expected approval dir to exist: %v", err)
	}
	if err := CleanupApprovalDir(dir); err != nil {
		t.Fatalf("CleanupApprovalDir: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("expected approval dir to be removed, got err=%v", err)
	}
}

func TestWriteAndReadRequestAndResponse(t *testing.T) {
	dir := t.TempDir()
	requestID, err := NewRequestID()
	if err != nil {
		t.Fatalf("NewRequestID: %v", err)
	}
	delegationID, err := NewDelegationID()
	if err != nil {
		t.Fatalf("NewDelegationID: %v", err)
	}
	req := Request{
		RequestID:    requestID,
		DelegationID: delegationID,
		Action:       "git_push_force",
		Description:  "Force-push branch",
		Context:      map[string]interface{}{"agent": "codex"},
	}
	if err := WriteRequest(dir, req); err != nil {
		t.Fatalf("WriteRequest: %v", err)
	}
	gotReq, err := ReadRequest(dir, req.RequestID)
	if err != nil {
		t.Fatalf("ReadRequest: %v", err)
	}
	if gotReq.Action != req.Action || gotReq.Description != req.Description {
		t.Fatalf("unexpected request contents: %#v", gotReq)
	}

	resp := Response{
		RequestID: req.RequestID,
		Decision:  "approved",
		DecidedBy: "user",
		Reason:    "confirmed",
	}
	if err := WriteResponse(dir, resp); err != nil {
		t.Fatalf("WriteResponse: %v", err)
	}
	gotResp, err := ReadResponse(dir, req.RequestID)
	if err != nil {
		t.Fatalf("ReadResponse: %v", err)
	}
	if gotResp.Decision != "approved" || gotResp.DecidedBy != "user" {
		t.Fatalf("unexpected response contents: %#v", gotResp)
	}
}

func TestWriteResponseRejectsDuplicate(t *testing.T) {
	dir := t.TempDir()
	requestID, err := NewRequestID()
	if err != nil {
		t.Fatalf("NewRequestID: %v", err)
	}
	resp := Response{
		RequestID: requestID,
		Decision:  "denied",
		DecidedBy: "user",
	}
	if err := WriteResponse(dir, resp); err != nil {
		t.Fatalf("first WriteResponse: %v", err)
	}
	if err := WriteResponse(dir, resp); err == nil {
		t.Fatalf("expected duplicate response write to fail")
	}
}

func TestReadMissingFilesReturnsError(t *testing.T) {
	dir := t.TempDir()
	if _, err := ReadRequest(dir, "req_00000000"); err == nil {
		t.Fatalf("expected missing request read to fail")
	}
	if _, err := ReadResponse(dir, "req_00000000"); err == nil {
		t.Fatalf("expected missing response read to fail")
	}
}

func TestWriteJSONAtomicLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	requestID, err := NewRequestID()
	if err != nil {
		t.Fatalf("NewRequestID: %v", err)
	}
	delegationID, err := NewDelegationID()
	if err != nil {
		t.Fatalf("NewDelegationID: %v", err)
	}
	req := Request{
		RequestID:    requestID,
		DelegationID: delegationID,
		Action:       "delete_file",
		Description:  "Delete file",
	}
	if err := WriteRequest(dir, req); err != nil {
		t.Fatalf("WriteRequest: %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(dir, "*.tmp-*"))
	if err != nil {
		t.Fatalf("glob tmp files: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("expected no temp files left behind, got %v", matches)
	}
}
