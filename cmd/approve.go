package cmd

import (
	"fmt"
	"os"

	"github.com/qq418716640/quancode/approval"
	"github.com/spf13/cobra"
)

var (
	approveAllow       bool
	approveDeny        bool
	approveReason      string
	approveApprovalDir string
)

var approveCmd = &cobra.Command{
	Use:   "approve <request-id>",
	Short: "Approve or deny a pending delegation approval request",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if approveAllow == approveDeny {
			return fmt.Errorf("exactly one of --allow or --deny must be provided")
		}

		approvalDir := approveApprovalDir
		if approvalDir == "" {
			approvalDir = os.Getenv("QUANCODE_APPROVAL_DIR")
		}
		if approvalDir == "" {
			return fmt.Errorf("approval directory not set; pass --approval-dir or set QUANCODE_APPROVAL_DIR")
		}

		requestID := args[0]
		if _, err := approval.ReadRequest(approvalDir, requestID); err != nil {
			return fmt.Errorf("read request %s: %w", requestID, err)
		}

		decision := "approved"
		if approveDeny {
			decision = "denied"
		}

		resp := approval.Response{
			RequestID: requestID,
			Decision:  decision,
			Reason:    approveReason,
			DecidedBy: "user",
		}
		if err := approval.WriteResponse(approvalDir, resp); err != nil {
			return err
		}

		label := "Approved"
		if decision == "denied" {
			label = "Denied"
		}
		fmt.Printf("%s %s\n", label, requestID)
		return nil
	},
}

func init() {
	approveCmd.Flags().BoolVar(&approveAllow, "allow", false, "approve the request")
	approveCmd.Flags().BoolVar(&approveDeny, "deny", false, "deny the request")
	approveCmd.Flags().StringVar(&approveReason, "reason", "", "optional reason to record with the decision")
	approveCmd.Flags().StringVar(&approveApprovalDir, "approval-dir", "", "approval directory (default: QUANCODE_APPROVAL_DIR)")
	rootCmd.AddCommand(approveCmd)
}
