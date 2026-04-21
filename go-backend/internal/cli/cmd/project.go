package cli

import (
	"fmt"

	"github.com/auto-developer-orchestrator/backend/internal/cli/api"
	"github.com/spf13/cobra"
)

var projectCmd = &cobra.Command{
	Use:   "project",
	Short: "Project commands",
}

var projectListCmd = &cobra.Command{
	Use:   "list",
	Short: "List projects",
	RunE: func(cmd *cobra.Command, args []string) error {
		client := api.NewClient(serverURL)
		var result api.ProjectListResponse
		if err := client.Get("/api/projects", &result); err != nil {
			return err
		}
		if outputFmt == "json" {
			return printJSON(result)
		}
		for _, p := range result.Projects {
			fmt.Println(p)
		}
		return nil
	},
}

var (
	projectPath   string
	projectRepoURL string
)

var projectAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Add a project",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		req := map[string]string{
			"name": args[0],
		}
		if projectPath != "" {
			req["path"] = projectPath
		}
		if projectRepoURL != "" {
			req["repoUrl"] = projectRepoURL
		}
		client := api.NewClient(serverURL)
		if err := client.Post("/api/projects/add", req, nil); err != nil {
			return err
		}
		fmt.Printf("Added project %s\n", args[0])
		return nil
	},
}

var projectCloneCmd = &cobra.Command{
	Use:   "clone <url>",
	Short: "Clone a repository",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := api.NewClient(serverURL)
		if err := client.Post("/api/clone", map[string]string{"url": args[0]}, nil); err != nil {
			return err
		}
		fmt.Println("Cloned successfully")
		return nil
	},
}

var projectStatusCmd = &cobra.Command{
	Use:   "status [name]",
	Short: "Get project status",
	RunE: func(cmd *cobra.Command, args []string) error {
		client := api.NewClient(serverURL)
		var result api.StatusResponse
		if err := client.Get("/api/status", &result); err != nil {
			return err
		}
		if outputFmt == "json" {
			return printJSON(result)
		}
		fmt.Printf("Branch: %s\n", result.Branch)
		if result.Modified > 0 || result.Staged > 0 {
			fmt.Printf("Modified: %d  Staged: %d\n", result.Modified, result.Staged)
		}
		return nil
	},
}

var projectBranchCmd = &cobra.Command{
	Use:   "branch",
	Short: "Show current branch",
	RunE: func(cmd *cobra.Command, args []string) error {
		client := api.NewClient(serverURL)
		var result struct {
			Branch string `json:"branch"`
		}
		if err := client.Get("/api/branch", &result); err != nil {
			return err
		}
		fmt.Println(result.Branch)
		return nil
	},
}

var (
	checkoutBranch string
)

var projectCheckoutCmd = &cobra.Command{
	Use:   "checkout",
	Short: "Checkout a branch",
	RunE: func(cmd *cobra.Command, args []string) error {
		if checkoutBranch == "" {
			return fmt.Errorf("--branch is required")
		}
		client := api.NewClient(serverURL)
		if err := client.Post("/api/branch/checkout", map[string]string{"branch": checkoutBranch}, nil); err != nil {
			return err
		}
		fmt.Printf("Checked out %s\n", checkoutBranch)
		return nil
	},
}

func init() {
	projectAddCmd.Flags().StringVar(&projectPath, "path", "", "Local path")
	projectAddCmd.Flags().StringVar(&projectRepoURL, "repo-url", "", "Repository URL")
	projectCheckoutCmd.Flags().StringVar(&checkoutBranch, "branch", "", "Branch name")
	projectCheckoutCmd.MarkFlagRequired("branch")

	projectCmd.AddCommand(projectListCmd)
	projectCmd.AddCommand(projectAddCmd)
	projectCmd.AddCommand(projectCloneCmd)
	projectCmd.AddCommand(projectStatusCmd)
	projectCmd.AddCommand(projectBranchCmd)
	projectCmd.AddCommand(projectCheckoutCmd)
	rootCmd.AddCommand(projectCmd)
}
