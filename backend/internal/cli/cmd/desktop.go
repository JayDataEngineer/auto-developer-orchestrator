package cli

import (
	"fmt"

	"github.com/auto-developer-orchestrator/backend/internal/cli/api"
	"github.com/spf13/cobra"
)

var desktopCmd = &cobra.Command{
	Use:   "desktop",
	Short: "Desktop automation commands",
}

var desktopEnableCmd = &cobra.Command{
	Use:   "enable <id>",
	Short: "Enable desktop mode on sandbox",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := api.NewClient(serverURL)
		var result map[string]interface{}
		if err := client.Post("/api/sandbox/"+args[0]+"/desktop-mode", nil, &result); err != nil {
			return err
		}
		fmt.Println("Desktop mode enabled")
		return nil
	},
}

var desktopDisableCmd = &cobra.Command{
	Use:   "disable <id>",
	Short: "Disable desktop mode on sandbox",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := api.NewClient(serverURL)
		if err := client.Delete("/api/sandbox/"+args[0]+"/mode", nil); err != nil {
			return err
		}
		fmt.Println("Desktop mode disabled")
		return nil
	},
}

var (
	desktopAction   string
	desktopElement  string
	desktopText     string
	desktopURL      string
	desktopKey      string
	desktopX        int
	desktopY        int
	desktopButton   string
	desktopDirection string
	desktopSubmit   bool
)

var desktopActCmd = &cobra.Command{
	Use:   "act <id>",
	Short: "Perform desktop action (click/type/scroll/navigate)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if desktopAction == "" {
			return fmt.Errorf("--action is required (click|type|scroll|navigate)")
		}
		req := map[string]interface{}{
			"action": desktopAction,
		}
		if desktopElement != "" {
			req["element"] = desktopElement
		}
		if desktopText != "" {
			req["text"] = desktopText
		}
		if desktopURL != "" {
			req["url"] = desktopURL
		}
		if desktopDirection != "" {
			req["direction"] = desktopDirection
		}
		req["submit"] = desktopSubmit

		client := api.NewClient(serverURL)
		var result map[string]interface{}
		if err := client.Post("/api/sandbox/"+args[0]+"/computer-use/act", req, &result); err != nil {
			return err
		}
		if outputFmt == "json" {
			return printJSON(result)
		}
		fmt.Println("Action performed")
		return nil
	},
}

var desktopMouseCmd = &cobra.Command{
	Use:   "mouse <id>",
	Short: "Mouse control (click/move)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		req := map[string]interface{}{
			"action": desktopAction,
			"x":      desktopX,
			"y":      desktopY,
		}
		if desktopButton != "" {
			req["button"] = desktopButton
		}
		client := api.NewClient(serverURL)
		if err := client.Post("/api/sandbox/"+args[0]+"/x11/mouse", req, nil); err != nil {
			return err
		}
		fmt.Println("Mouse action performed")
		return nil
	},
}

var desktopKeyboardCmd = &cobra.Command{
	Use:   "keyboard <id>",
	Short: "Keyboard input",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		req := map[string]interface{}{}
		if desktopText != "" {
			req["type"] = "type"
			req["text"] = desktopText
		} else if desktopKey != "" {
			req["type"] = "key"
			req["key"] = desktopKey
		} else {
			return fmt.Errorf("--type or --key is required")
		}
		client := api.NewClient(serverURL)
		if err := client.Post("/api/sandbox/"+args[0]+"/x11/keyboard", req, nil); err != nil {
			return err
		}
		fmt.Println("Keyboard action performed")
		return nil
	},
}

var desktopSnapshotCmd = &cobra.Command{
	Use:   "snapshot <id>",
	Short: "Get accessibility snapshot",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := api.NewClient(serverURL)
		var result map[string]interface{}
		if err := client.Get("/api/sandbox/"+args[0]+"/computer-use/snapshot", &result); err != nil {
			return err
		}
		return printJSON(result)
	},
}

func init() {
	desktopActCmd.Flags().StringVar(&desktopAction, "action", "", "Action: click|type|scroll|navigate")
	desktopActCmd.Flags().StringVar(&desktopElement, "element", "", "Element ID")
	desktopActCmd.Flags().StringVar(&desktopText, "text", "", "Text to type")
	desktopActCmd.Flags().StringVar(&desktopURL, "url", "", "URL to navigate to")
	desktopActCmd.Flags().StringVar(&desktopDirection, "direction", "", "Scroll direction")
	desktopActCmd.Flags().BoolVar(&desktopSubmit, "submit", false, "Submit after typing")

	desktopMouseCmd.Flags().StringVar(&desktopAction, "action", "click", "Action: click|move")
	desktopMouseCmd.Flags().IntVar(&desktopX, "x", 0, "X coordinate")
	desktopMouseCmd.Flags().IntVar(&desktopY, "y", 0, "Y coordinate")
	desktopMouseCmd.Flags().StringVar(&desktopButton, "button", "left", "Button: left|right|middle")

	desktopKeyboardCmd.Flags().StringVar(&desktopText, "type", "", "Type text")
	desktopKeyboardCmd.Flags().StringVar(&desktopKey, "key", "", "Press key (e.g. Return, Escape)")

	desktopCmd.AddCommand(desktopEnableCmd)
	desktopCmd.AddCommand(desktopDisableCmd)
	desktopCmd.AddCommand(desktopActCmd)
	desktopCmd.AddCommand(desktopMouseCmd)
	desktopCmd.AddCommand(desktopKeyboardCmd)
	desktopCmd.AddCommand(desktopSnapshotCmd)
	rootCmd.AddCommand(desktopCmd)
}
