package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/auto-developer-orchestrator/backend/internal/cli/api"
	"github.com/spf13/cobra"
)

var sandboxCmd = &cobra.Command{
	Use:   "sandbox",
	Short: "Sandbox commands",
}

var sandboxListCmd = &cobra.Command{
	Use:   "list",
	Short: "List sandboxes",
	RunE: func(cmd *cobra.Command, args []string) error {
		client := api.NewClient(serverURL)
		var result []api.SandboxInfo
		if err := client.Get("/api/sandbox/", &result); err != nil {
			return err
		}
		if outputFmt == "json" {
			return printJSON(result)
		}
		if len(result) == 0 {
			fmt.Println("No sandboxes.")
			return nil
		}
		for _, s := range result {
			fmt.Printf("%-20s  status:%s  image:%s\n", s.ID, s.Status, s.Image)
		}
		return nil
	},
}

var (
	sandboxID       string
	sandboxProjPath string
)

var sandboxCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a sandbox",
	RunE: func(cmd *cobra.Command, args []string) error {
		req := map[string]string{
			"id": sandboxID,
		}
		if sandboxProjPath != "" {
			req["project_path"] = sandboxProjPath
		}
		// Thread the global --org flag through so `orch sandbox create --org X`
		// honors org-declared sandbox image/env/volumes. Without this, the
		// standalone create launches a generic pux-sandbox container even
		// when the org declares a specialized image (the same gap the prompt
		// path closed in pux_prompt.go). Resolve the name to an absolute path
		// so the server-side LoadOrgManifest finds pux.yaml regardless of
		// the server's working directory.
		if orgName != "" {
			orgPath, err := resolveOrgPath(orgName)
			if err != nil {
				return err
			}
			req["org"] = orgPath
		}
		client := api.NewClient(serverURL)
		var result api.SandboxInfo
		if err := client.Post("/api/sandbox/", req, &result); err != nil {
			return err
		}
		if outputFmt == "json" {
			return printJSON(result)
		}
		fmt.Printf("Created sandbox %s\n", result.ID)
		return nil
	},
}

var sandboxExecCmd = &cobra.Command{
	Use:   "exec <id> -- <command>",
	Short: "Execute command in sandbox",
	Args:  cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := args[0]
		command := args[1]
		client := api.NewClient(serverURL)
		var result api.ExecResult
		if err := client.Post("/api/sandbox/"+id+"/exec", map[string]string{
			"command": command,
		}, &result); err != nil {
			return err
		}
		if outputFmt == "json" {
			return printJSON(result)
		}
		if result.Stdout != "" {
			fmt.Print(result.Stdout)
		}
		if result.Stderr != "" {
			fmt.Fprint(os.Stderr, result.Stderr)
		}
		if result.ExitCode != 0 {
			return fmt.Errorf("exit code %d", result.ExitCode)
		}
		return nil
	},
}

var sandboxDestroyCmd = &cobra.Command{
	Use:   "destroy <id>",
	Short: "Destroy a sandbox",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := api.NewClient(serverURL)
		if err := client.Delete("/api/sandbox/"+args[0], nil); err != nil {
			return err
		}
		fmt.Printf("Destroyed sandbox %s\n", args[0])
		return nil
	},
}

var sandboxReadyCmd = &cobra.Command{
	Use:   "ready <id>",
	Short: "Check if sandbox is ready",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := api.NewClient(serverURL)
		var result struct {
			Ready bool `json:"ready"`
		}
		if err := client.Get("/api/sandbox/"+args[0]+"/ready", &result); err != nil {
			return err
		}
		if outputFmt == "json" {
			return printJSON(result)
		}
		if result.Ready {
			fmt.Println("Ready")
		} else {
			fmt.Println("Not ready")
		}
		return nil
	},
}

var sandboxScreenshotCmd = &cobra.Command{
	Use:   "screenshot <id>",
	Short: "Take a screenshot of sandbox desktop",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := api.NewClient(serverURL)
		resp, err := client.StreamGet("/api/sandbox/" + args[0] + "/x11/screenshot")
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if screenshotSave != "" {
			f, err := os.Create(screenshotSave)
			if err != nil {
				return err
			}
			defer f.Close()
			_, err = io.Copy(f, resp.Body)
			fmt.Printf("Screenshot saved to %s\n", screenshotSave)
			return err
		}
		_, err = io.Copy(os.Stdout, resp.Body)
		return err
	},
}

var screenshotSave string

func init() {
	sandboxCreateCmd.Flags().StringVar(&sandboxID, "id", "", "Sandbox ID")
	sandboxCreateCmd.Flags().StringVar(&sandboxProjPath, "project-path", "", "Project path to bind")
	sandboxCreateCmd.MarkFlagRequired("id")
	sandboxScreenshotCmd.Flags().StringVar(&screenshotSave, "save", "", "Save to file")

	sandboxCmd.AddCommand(sandboxListCmd)
	sandboxCmd.AddCommand(sandboxCreateCmd)
	sandboxCmd.AddCommand(sandboxExecCmd)
	sandboxCmd.AddCommand(sandboxDestroyCmd)
	sandboxCmd.AddCommand(sandboxReadyCmd)
	sandboxCmd.AddCommand(sandboxScreenshotCmd)
	rootCmd.AddCommand(sandboxCmd)
}
