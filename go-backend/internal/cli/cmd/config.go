package cli

import (
	"fmt"

	"github.com/auto-developer-orchestrator/backend/internal/cli/api"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Configuration commands",
}

var configAICmd = &cobra.Command{
	Use:   "ai",
	Short: "Get or set AI configuration",
	RunE: func(cmd *cobra.Command, args []string) error {
		client := api.NewClient(serverURL)
		if len(configSetArgs) > 0 {
			req := make(map[string]interface{})
			for k, v := range configSetArgs {
				req[k] = v
			}
			if err := client.Post("/api/config/ai", req, nil); err != nil {
				return err
			}
			fmt.Println("Updated")
			return nil
		}
		var result map[string]interface{}
		if err := client.Get("/api/config/ai", &result); err != nil {
			return err
		}
		return printJSON(result)
	},
}

var configModelsCmd = &cobra.Command{
	Use:   "models",
	Short: "Get or set model configuration",
	RunE: func(cmd *cobra.Command, args []string) error {
		client := api.NewClient(serverURL)
		if len(configSetArgs) > 0 {
			req := make(map[string]interface{})
			for k, v := range configSetArgs {
				req[k] = v
			}
			if err := client.Put("/api/config/models", req, nil); err != nil {
				return err
			}
			fmt.Println("Updated")
			return nil
		}
		var result map[string]interface{}
		if err := client.Get("/api/config/models", &result); err != nil {
			return err
		}
		return printJSON(result)
	},
}

var configSystemCmd = &cobra.Command{
	Use:   "system",
	Short: "Get or set system configuration",
	RunE: func(cmd *cobra.Command, args []string) error {
		client := api.NewClient(serverURL)
		if len(configSetArgs) > 0 {
			req := make(map[string]interface{})
			for k, v := range configSetArgs {
				req[k] = v
			}
			if err := client.Post("/api/config/system", req, nil); err != nil {
				return err
			}
			fmt.Println("Updated")
			return nil
		}
		var result map[string]interface{}
		if err := client.Get("/api/config/system", &result); err != nil {
			return err
		}
		return printJSON(result)
	},
}

var configProjectCmd = &cobra.Command{
	Use:   "project",
	Short: "Get or set project configuration",
	RunE: func(cmd *cobra.Command, args []string) error {
		client := api.NewClient(serverURL)
		if len(configSetArgs) > 0 {
			req := make(map[string]interface{})
			for k, v := range configSetArgs {
				req[k] = v
			}
			if err := client.Put("/api/config/project", req, nil); err != nil {
				return err
			}
			fmt.Println("Updated")
			return nil
		}
		var result map[string]interface{}
		if err := client.Get("/api/config/project", &result); err != nil {
			return err
		}
		return printJSON(result)
	},
}

var configSetArgs map[string]string

func init() {
	for _, c := range []*cobra.Command{configAICmd, configModelsCmd, configSystemCmd, configProjectCmd} {
		c.Flags().StringToStringVar(&configSetArgs, "set", nil, "Set key=value (can repeat)")
	}

	configCmd.AddCommand(configAICmd)
	configCmd.AddCommand(configModelsCmd)
	configCmd.AddCommand(configSystemCmd)
	configCmd.AddCommand(configProjectCmd)
	rootCmd.AddCommand(configCmd)
}
