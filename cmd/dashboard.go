package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/qq418716640/quancode/server"
	"github.com/spf13/cobra"
)

var (
	dashboardPort int
	dashboardDev  bool
	dashboardOpen bool
)

var dashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Start the web dashboard for monitoring delegations and jobs",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		addr := fmt.Sprintf("127.0.0.1:%d", dashboardPort)
		srv := server.New(addr, dashboardDev)

		url := fmt.Sprintf("http://%s", addr)
		if dashboardDev {
			fmt.Fprintf(os.Stderr, "Dashboard (dev mode): %s\n", url)
		} else {
			fmt.Fprintf(os.Stderr, "Dashboard: %s\n", url)
		}

		if dashboardOpen {
			openBrowser(url)
		}

		return srv.ListenAndServe()
	},
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return
	}
	cmd.Start()
}

func init() {
	dashboardCmd.Flags().IntVar(&dashboardPort, "port", 8377, "port to listen on")
	dashboardCmd.Flags().BoolVar(&dashboardDev, "dev", false, "serve static files from filesystem instead of embedded assets")
	dashboardCmd.Flags().BoolVar(&dashboardOpen, "open", false, "open browser automatically after starting")
	rootCmd.AddCommand(dashboardCmd)
}
