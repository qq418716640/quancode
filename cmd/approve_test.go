package cmd

import (
	"os"
	"strings"
	"testing"

	"github.com/qq418716640/quancode/approval"
)

func TestApproveCmdWritesAllowResponse(t *testing.T) {
	dir := t.TempDir()
	requestID, err := approval.NewRequestID()
	if err != nil {
		t.Fatalf("NewRequestID: %v", err)
	}
	delegationID, err := approval.NewDelegationID()
	if err != nil {
		t.Fatalf("NewDelegationID: %v", err)
	}
	req := approval.Request{
		RequestID:    requestID,
		DelegationID: delegationID,
		Action:       "git_push_force",
		Description:  "Force-push branch",
	}
	if err := approval.WriteRequest(dir, req); err != nil {
		t.Fatalf("WriteRequest: %v", err)
	}

	oldAllow, oldDeny, oldReason, oldDir := approveAllow, approveDeny, approveReason, approveApprovalDir
	approveAllow, approveDeny, approveReason, approveApprovalDir = true, false, "confirmed", dir
	defer func() {
		approveAllow, approveDeny, approveReason, approveApprovalDir = oldAllow, oldDeny, oldReason, oldDir
	}()

	out := captureStdout(t, func() {
		if err := approveCmd.RunE(approveCmd, []string{req.RequestID}); err != nil {
			t.Fatalf("approve RunE: %v", err)
		}
	})
	if !strings.Contains(out, "Approved") {
		t.Fatalf("expected approval confirmation, got %q", out)
	}

	resp, err := approval.ReadResponse(dir, req.RequestID)
	if err != nil {
		t.Fatalf("ReadResponse: %v", err)
	}
	if resp.Decision != "approved" || resp.DecidedBy != "user" {
		t.Fatalf("unexpected response: %#v", resp)
	}
}

func TestApproveCmdWritesDenyResponseFromEnv(t *testing.T) {
	dir := t.TempDir()
	requestID, err := approval.NewRequestID()
	if err != nil {
		t.Fatalf("NewRequestID: %v", err)
	}
	delegationID, err := approval.NewDelegationID()
	if err != nil {
		t.Fatalf("NewDelegationID: %v", err)
	}
	req := approval.Request{
		RequestID:    requestID,
		DelegationID: delegationID,
		Action:       "delete_file",
		Description:  "Delete file",
	}
	if err := approval.WriteRequest(dir, req); err != nil {
		t.Fatalf("WriteRequest: %v", err)
	}

	oldAllow, oldDeny, oldReason, oldDir := approveAllow, approveDeny, approveReason, approveApprovalDir
	approveAllow, approveDeny, approveReason, approveApprovalDir = false, true, "", ""
	defer func() {
		approveAllow, approveDeny, approveReason, approveApprovalDir = oldAllow, oldDeny, oldReason, oldDir
	}()

	oldEnv := os.Getenv("QUANCODE_APPROVAL_DIR")
	if err := os.Setenv("QUANCODE_APPROVAL_DIR", dir); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
	defer os.Setenv("QUANCODE_APPROVAL_DIR", oldEnv)

	if err := approveCmd.RunE(approveCmd, []string{req.RequestID}); err != nil {
		t.Fatalf("approve RunE: %v", err)
	}

	resp, err := approval.ReadResponse(dir, req.RequestID)
	if err != nil {
		t.Fatalf("ReadResponse: %v", err)
	}
	if resp.Decision != "denied" {
		t.Fatalf("expected denied response, got %#v", resp)
	}
}

func TestApproveCmdRequiresRequestFile(t *testing.T) {
	oldAllow, oldDeny, oldReason, oldDir := approveAllow, approveDeny, approveReason, approveApprovalDir
	approveAllow, approveDeny, approveReason, approveApprovalDir = true, false, "", t.TempDir()
	defer func() {
		approveAllow, approveDeny, approveReason, approveApprovalDir = oldAllow, oldDeny, oldReason, oldDir
	}()

	if err := approveCmd.RunE(approveCmd, []string{"req_missing"}); err == nil {
		t.Fatalf("expected missing request to fail")
	}
}

func TestApproveCmdRejectsDuplicateResponse(t *testing.T) {
	dir := t.TempDir()
	requestID, err := approval.NewRequestID()
	if err != nil {
		t.Fatalf("NewRequestID: %v", err)
	}
	delegationID, err := approval.NewDelegationID()
	if err != nil {
		t.Fatalf("NewDelegationID: %v", err)
	}
	req := approval.Request{
		RequestID:    requestID,
		DelegationID: delegationID,
		Action:       "git_push_force",
		Description:  "Force-push branch",
	}
	if err := approval.WriteRequest(dir, req); err != nil {
		t.Fatalf("WriteRequest: %v", err)
	}
	if err := approval.WriteResponse(dir, approval.Response{
		RequestID: req.RequestID,
		Decision:  "approved",
		DecidedBy: "user",
	}); err != nil {
		t.Fatalf("WriteResponse: %v", err)
	}

	oldAllow, oldDeny, oldReason, oldDir := approveAllow, approveDeny, approveReason, approveApprovalDir
	approveAllow, approveDeny, approveReason, approveApprovalDir = true, false, "", dir
	defer func() {
		approveAllow, approveDeny, approveReason, approveApprovalDir = oldAllow, oldDeny, oldReason, oldDir
	}()

	if err := approveCmd.RunE(approveCmd, []string{req.RequestID}); err == nil {
		t.Fatalf("expected duplicate response write to fail")
	}
}

func TestApproveCmdRequiresExactlyOneDecisionFlag(t *testing.T) {
	dir := t.TempDir()
	oldAllow, oldDeny, oldReason, oldDir := approveAllow, approveDeny, approveReason, approveApprovalDir
	defer func() {
		approveAllow, approveDeny, approveReason, approveApprovalDir = oldAllow, oldDeny, oldReason, oldDir
	}()

	approveAllow, approveDeny, approveApprovalDir = false, false, dir
	if err := approveCmd.RunE(approveCmd, []string{"req_any"}); err == nil {
		t.Fatalf("expected no decision flags to fail")
	}

	approveAllow, approveDeny, approveApprovalDir = true, true, dir
	if err := approveCmd.RunE(approveCmd, []string{"req_any"}); err == nil {
		t.Fatalf("expected both decision flags to fail")
	}
}
