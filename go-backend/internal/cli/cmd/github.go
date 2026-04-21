package cli

import (
	"fmt"

	"github.com/auto-developer-orchestrator/backend/internal/cli/api"
	"github.com/spf13/cobra"
)

var githubCmd = &cobra.Command{
	Use:   "github",
	Short: "GitHub integration commands",
}

var (
	githubToken string
	githubOwner string
	githubRepo  string
)

var githubConnectCmd = &cobra.Command{
	Use:   "connect",
	Short: "Connect GitHub account",
	RunE: func(cmd *cobra.Command, args []string) error {
		if githubToken == "" {
			return fmt.Errorf("--token is required")
		}
		client := api.NewClient(serverURL)
		if err := client.Post("/api/config/github", map[string]string{"token": githubToken}, nil); err != nil {
			return err
		}
		fmt.Println("GitHub connected")
		return nil
	},
}

var githubUserCmd = &cobra.Command{
	Use:   "user",
	Short: "Get authenticated GitHub user",
	RunE: func(cmd *cobra.Command, args []string) error {
		client := api.NewClient(serverURL)
		var result map[string]interface{}
		if err := client.Get("/api/github/user", &result); err != nil {
			return err
		}
		return printJSON(result)
	},
}

var githubReposCmd = &cobra.Command{
	Use:   "repos",
	Short: "List GitHub repositories",
	RunE: func(cmd *cobra.Command, args []string) error {
		client := api.NewClient(serverURL)
		var result []map[string]interface{}
		if err := client.Get("/api/github/repos", &result); err != nil {
			return err
		}
		if outputFmt == "json" {
			return printJSON(result)
		}
		for _, r := range result {
			name, _ := r["full_name"].(string)
			private, _ := r["private"].(bool)
			fmt.Printf("%s  private:%v\n", name, private)
		}
		return nil
	},
}

var githubPRsCmd = &cobra.Command{
	Use:   "prs",
	Short: "List pull requests",
	RunE: func(cmd *cobra.Command, args []string) error {
		if githubOwner == "" || githubRepo == "" {
			return fmt.Errorf("--owner and --repo are required")
		}
		client := api.NewClient(serverURL)
		var result []map[string]interface{}
		if err := client.Get(fmt.Sprintf("/api/github/prs?owner=%s&repo=%s", githubOwner, githubRepo), &result); err != nil {
			return err
		}
		if outputFmt == "json" {
			return printJSON(result)
		}
		for _, pr := range result {
			num, _ := pr["number"].(float64)
			title, _ := pr["title"].(string)
			state, _ := pr["state"].(string)
			fmt.Printf("#%.0f  %s  %s\n", num, state, title)
		}
		return nil
	},
}

var githubBranchesCmd = &cobra.Command{
	Use:   "branches",
	Short: "List repository branches",
	RunE: func(cmd *cobra.Command, args []string) error {
		if githubOwner == "" || githubRepo == "" {
			return fmt.Errorf("--owner and --repo are required")
		}
		client := api.NewClient(serverURL)
		var result []map[string]interface{}
		if err := client.Get(fmt.Sprintf("/api/github/branches?owner=%s&repo=%s", githubOwner, githubRepo), &result); err != nil {
			return err
		}
		if outputFmt == "json" {
			return printJSON(result)
		}
		for _, b := range result {
			name, _ := b["name"].(string)
			fmt.Println(name)
		}
		return nil
	},
}

var githubStatsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Get repository stats",
	RunE: func(cmd *cobra.Command, args []string) error {
		if githubOwner == "" || githubRepo == "" {
			return fmt.Errorf("--owner and --repo are required")
		}
		client := api.NewClient(serverURL)
		var result map[string]interface{}
		if err := client.Get(fmt.Sprintf("/api/github/stats?owner=%s&repo=%s", githubOwner, githubRepo), &result); err != nil {
			return err
		}
		return printJSON(result)
	},
}

var githubActivityCmd = &cobra.Command{
	Use:   "activity",
	Short: "Get repository activity",
	RunE: func(cmd *cobra.Command, args []string) error {
		if githubOwner == "" || githubRepo == "" {
			return fmt.Errorf("--owner and --repo are required")
		}
		client := api.NewClient(serverURL)
		var result []map[string]interface{}
		if err := client.Get(fmt.Sprintf("/api/github/activity?owner=%s&repo=%s", githubOwner, githubRepo), &result); err != nil {
			return err
		}
		return printJSON(result)
	},
}

func init() {
	githubConnectCmd.Flags().StringVar(&githubToken, "token", "", "GitHub personal access token")
	githubConnectCmd.MarkFlagRequired("token")

	for _, c := range []*cobra.Command{githubPRsCmd, githubBranchesCmd, githubStatsCmd, githubActivityCmd} {
		c.Flags().StringVar(&githubOwner, "owner", "", "Repository owner")
		c.Flags().StringVar(&githubRepo, "repo", "", "Repository name")
	}

	githubCmd.AddCommand(githubConnectCmd)
	githubCmd.AddCommand(githubUserCmd)
	githubCmd.AddCommand(githubReposCmd)
	githubCmd.AddCommand(githubPRsCmd)
	githubCmd.AddCommand(githubBranchesCmd)
	githubCmd.AddCommand(githubStatsCmd)
	githubCmd.AddCommand(githubActivityCmd)
	rootCmd.AddCommand(githubCmd)
}
