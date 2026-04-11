package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/qq418716640/quancode/config"
	"github.com/qq418716640/quancode/dashboard"
	"github.com/qq418716640/quancode/server"
	"github.com/spf13/cobra"
)

var (
	dashboardPort int
	dashboardDev  bool
	dashboardOpen bool
	dashboardDemo bool
)

var dashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Start the web dashboard for monitoring delegations and jobs",
	RunE: func(cmd *cobra.Command, args []string) error {
		addr := fmt.Sprintf("127.0.0.1:%d", dashboardPort)
		var srv *server.Server
		if dashboardDemo {
			srv = server.NewDemo(addr)
		} else {
			srv = server.New(addr, dashboardDev)
		}

		url := fmt.Sprintf("http://%s", addr)
		switch {
		case dashboardDemo:
			fmt.Fprintf(os.Stderr, "Dashboard (demo): %s\n", url)
		case dashboardDev:
			fmt.Fprintf(os.Stderr, "Dashboard (dev mode): %s\n", url)
		default:
			fmt.Fprintf(os.Stderr, "Dashboard: %s\n", url)
		}

		if dashboardOpen {
			openBrowser(url)
		}

		return srv.ListenAndServe()
	},
}

var dashboardEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Enable dashboard auto-start when running quancode start",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := config.UpdateDashboardMode(cfgFile, "auto"); err != nil {
			return fmt.Errorf("update config: %w", err)
		}
		fmt.Fprintln(os.Stderr, "[quancode] dashboard auto-start enabled")

		// Also start it now if not already running.
		cfg, err := config.Load(cfgFile)
		if err != nil {
			return nil // config saved, non-fatal
		}
		port := cfg.Preferences.EffectiveDashboardPort()
		url, started, err := dashboard.EnsureRunning(port)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[quancode] warning: could not start dashboard now: %v\n", err)
			return nil
		}
		if started {
			fmt.Fprintf(os.Stderr, "[quancode] dashboard started: %s\n", url)
		} else {
			fmt.Fprintf(os.Stderr, "[quancode] dashboard already running: %s\n", url)
		}
		return nil
	},
}

var dashboardDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Disable dashboard auto-start",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := config.UpdateDashboardMode(cfgFile, "off"); err != nil {
			return fmt.Errorf("update config: %w", err)
		}
		fmt.Fprintln(os.Stderr, "[quancode] dashboard auto-start disabled")
		return nil
	},
}

var dashboardStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show dashboard auto-start preference and running state",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(cfgFile)
		if err != nil {
			return err
		}
		mode := cfg.Preferences.DashboardMode
		if mode == "" {
			mode = "(not configured)"
		}
		port := cfg.Preferences.EffectiveDashboardPort()

		fmt.Fprintf(os.Stdout, "mode:    %s\n", mode)
		fmt.Fprintf(os.Stdout, "port:    %d\n", port)
		if dashboard.Probe(port) {
			fmt.Fprintf(os.Stdout, "running: yes (http://127.0.0.1:%d)\n", port)
		} else {
			fmt.Fprintf(os.Stdout, "running: no\n")
		}
		return nil
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
	dashboardCmd.Flags().IntVar(&dashboardPort, "port", config.DefaultDashboardPort, "port to listen on")
	dashboardCmd.Flags().BoolVar(&dashboardDev, "dev", false, "serve static files from filesystem instead of embedded assets")
	dashboardCmd.Flags().BoolVar(&dashboardOpen, "open", false, "open browser automatically after starting")
	dashboardCmd.Flags().BoolVar(&dashboardDemo, "demo", false, "use built-in demo data instead of real logs")
	dashboardCmd.AddCommand(dashboardEnableCmd)
	dashboardCmd.AddCommand(dashboardDisableCmd)
	dashboardCmd.AddCommand(dashboardStatusCmd)
	rootCmd.AddCommand(dashboardCmd)
}
