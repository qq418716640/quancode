package approval

import "testing"

func TestPendingRequestsReturnsUnansweredOnly(t *testing.T) {
	dir := t.TempDir()
	requestID1, err := NewRequestID()
	if err != nil {
		t.Fatalf("NewRequestID req1: %v", err)
	}
	delegationID1, err := NewDelegationID()
	if err != nil {
		t.Fatalf("NewDelegationID req1: %v", err)
	}
	requestID2, err := NewRequestID()
	if err != nil {
		t.Fatalf("NewRequestID req2: %v", err)
	}
	delegationID2, err := NewDelegationID()
	if err != nil {
		t.Fatalf("NewDelegationID req2: %v", err)
	}

	req1 := Request{
		RequestID:    requestID1,
		DelegationID: delegationID1,
		Action:       "delete_file",
		Description:  "Delete file",
	}
	req2 := Request{
		RequestID:    requestID2,
		DelegationID: delegationID2,
		Action:       "git_push_force",
		Description:  "Force-push branch",
	}
	if err := WriteRequest(dir, req1); err != nil {
		t.Fatalf("WriteRequest req1: %v", err)
	}
	if err := WriteRequest(dir, req2); err != nil {
		t.Fatalf("WriteRequest req2: %v", err)
	}
	if err := WriteResponse(dir, Response{
		RequestID: req1.RequestID,
		Decision:  "approved",
		DecidedBy: "user",
	}); err != nil {
		t.Fatalf("WriteResponse req1: %v", err)
	}

	pending, err := PendingRequests(dir)
	if err != nil {
		t.Fatalf("PendingRequests: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending request, got %d", len(pending))
	}
	if pending[0].RequestID != req2.RequestID {
		t.Fatalf("expected pending request %q, got %q", req2.RequestID, pending[0].RequestID)
	}
}

func TestPendingRequestsEmptyDir(t *testing.T) {
	pending, err := PendingRequests(t.TempDir())
	if err != nil {
		t.Fatalf("PendingRequests: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("expected no pending requests, got %d", len(pending))
	}
}

func TestResponseExists(t *testing.T) {
	dir := t.TempDir()
	requestID, err := NewRequestID()
	if err != nil {
		t.Fatalf("NewRequestID: %v", err)
	}
	ok, err := ResponseExists(dir, requestID)
	if err != nil {
		t.Fatalf("ResponseExists before write: %v", err)
	}
	if ok {
		t.Fatalf("expected response not to exist yet")
	}

	if err := WriteResponse(dir, Response{
		RequestID: requestID,
		Decision:  "denied",
		DecidedBy: "timeout",
	}); err != nil {
		t.Fatalf("WriteResponse: %v", err)
	}
	ok, err = ResponseExists(dir, requestID)
	if err != nil {
		t.Fatalf("ResponseExists after write: %v", err)
	}
	if !ok {
		t.Fatalf("expected response to exist")
	}
}
