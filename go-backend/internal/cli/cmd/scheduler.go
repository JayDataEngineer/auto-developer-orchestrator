package cli

import (
	"fmt"

	"github.com/auto-developer-orchestrator/backend/internal/cli/api"
	"github.com/auto-developer-orchestrator/backend/internal/util"
	"github.com/spf13/cobra"
)

var schedulerCmd = &cobra.Command{
	Use:   "scheduler",
	Short: "Scheduler commands",
}

var schedulerListCmd = &cobra.Command{
	Use:   "list",
	Short: "List scheduled jobs",
	RunE: func(cmd *cobra.Command, args []string) error {
		client := api.NewClient(serverURL)
		var result api.SchedulerListResponse
		if err := client.Get("/api/scheduler/", &result); err != nil {
			return err
		}
		if outputFmt == "json" {
			return printJSON(result.Jobs)
		}
		if len(result.Jobs) == 0 {
			fmt.Println("No scheduled jobs.")
			return nil
		}
		for _, j := range result.Jobs {
			schedule := j.CronExpr
			if schedule == "" {
				schedule = j.ScheduleType
			}
			fmt.Printf("%s  %-20s  %s  %s  enabled:%v\n",
				j.ID[:8], util.TruncateEllipsis(j.Name, 20), j.Project, schedule, j.Enabled)
		}
		return nil
	},
}

var (
	schedName        string
	schedProject     string
	schedMessage     string
	schedType        string
	schedCron        string
	schedEvery       string
	schedAt          string
	schedEnabled     bool
	schedAutoBranch  bool
	schedThinking    string
	schedModel       string
	schedWebhook     bool
)

var schedulerCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a scheduled job",
	RunE: func(cmd *cobra.Command, args []string) error {
		req := api.CreateJobRequest{
			Name:          schedName,
			Project:       schedProject,
			Message:       schedMessage,
			ScheduleType:  schedType,
			CronExpr:      schedCron,
			EveryInterval: schedEvery,
			AtTime:        schedAt,
			Enabled:       schedEnabled,
			AutoBranch:    schedAutoBranch,
			ThinkingLevel: schedThinking,
			Model:         schedModel,
			Webhook:       schedWebhook,
		}
		client := api.NewClient(serverURL)
		var result api.SchedulerJob
		if err := client.Post("/api/scheduler/", req, &result); err != nil {
			return err
		}
		if outputFmt == "json" {
			return printJSON(result)
		}
		fmt.Printf("Created job %s (%s)\n", result.ID, result.Name)
		return nil
	},
}

var schedulerGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get job details",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := api.NewClient(serverURL)
		var result api.SchedulerJob
		if err := client.Get("/api/scheduler/"+args[0], &result); err != nil {
			return err
		}
		return printJSON(result)
	},
}

var schedulerUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update a scheduled job",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		req := api.CreateJobRequest{
			Name:          schedName,
			Project:       schedProject,
			Message:       schedMessage,
			ScheduleType:  schedType,
			CronExpr:      schedCron,
			EveryInterval: schedEvery,
			AtTime:        schedAt,
			Enabled:       schedEnabled,
			AutoBranch:    schedAutoBranch,
			ThinkingLevel: schedThinking,
			Model:         schedModel,
		}
		client := api.NewClient(serverURL)
		var result api.SchedulerJob
		if err := client.Put("/api/scheduler/"+args[0], req, &result); err != nil {
			return err
		}
		if outputFmt == "json" {
			return printJSON(result)
		}
		fmt.Printf("Updated job %s\n", args[0])
		return nil
	},
}

var schedulerDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a scheduled job",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := api.NewClient(serverURL)
		if err := client.Delete("/api/scheduler/"+args[0], nil); err != nil {
			return err
		}
		fmt.Printf("Deleted job %s\n", args[0])
		return nil
	},
}

var schedulerTriggerCmd = &cobra.Command{
	Use:   "trigger <id>",
	Short: "Manually trigger a job",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := api.NewClient(serverURL)
		var result map[string]interface{}
		if err := client.Post("/api/scheduler/"+args[0]+"/trigger", nil, &result); err != nil {
			return err
		}
		if outputFmt == "json" {
			return printJSON(result)
		}
		fmt.Printf("Triggered job %s\n", args[0])
		return nil
	},
}

var schedulerRunsCmd = &cobra.Command{
	Use:   "runs [id]",
	Short: "List job executions",
	RunE: func(cmd *cobra.Command, args []string) error {
		client := api.NewClient(serverURL)
		path := "/api/scheduler/runs"
		if len(args) > 0 {
			path = "/api/scheduler/" + args[0] + "/runs"
		}
		var result api.SchedulerRunsResponse
		if err := client.Get(path, &result); err != nil {
			return err
		}
		if outputFmt == "json" {
			return printJSON(result.Runs)
		}
		if len(result.Runs) == 0 {
			fmt.Println("No runs found.")
			return nil
		}
		for _, r := range result.Runs {
			id := r.ID
			jobID := r.JobID
			if len(id) > 8 {
				id = id[:8]
			}
			if len(jobID) > 8 {
				jobID = jobID[:8]
			}
			fmt.Printf("%s  job:%s  status:%s  started:%s\n",
				id, jobID, r.Status, r.StartedAt)
		}
		return nil
	},
}

func init() {
	schedulerCreateCmd.Flags().StringVar(&schedName, "name", "", "Job name")
	schedulerCreateCmd.Flags().StringVar(&schedProject, "project", "", "Project")
	schedulerCreateCmd.Flags().StringVar(&schedMessage, "message", "", "Prompt message")
	schedulerCreateCmd.Flags().StringVar(&schedType, "schedule-type", "manual", "Schedule type: cron|every|at|manual")
	schedulerCreateCmd.Flags().StringVar(&schedCron, "cron", "", "Cron expression")
	schedulerCreateCmd.Flags().StringVar(&schedEvery, "every", "", "Interval (e.g. 1h, 30m)")
	schedulerCreateCmd.Flags().StringVar(&schedAt, "at", "", "Time to run (e.g. 2025-01-01T10:00:00)")
	schedulerCreateCmd.Flags().BoolVar(&schedEnabled, "enabled", true, "Enable immediately")
	schedulerCreateCmd.Flags().BoolVar(&schedAutoBranch, "auto-branch", false, "Auto-create branch")
	schedulerCreateCmd.Flags().StringVar(&schedThinking, "thinking-level", "", "Thinking level")
	schedulerCreateCmd.Flags().StringVar(&schedModel, "model", "", "Model override")
	schedulerCreateCmd.Flags().BoolVar(&schedWebhook, "webhook", false, "Generate webhook token")
	schedulerCreateCmd.MarkFlagRequired("name")
	schedulerCreateCmd.MarkFlagRequired("message")

	schedulerUpdateCmd.Flags().AddFlagSet(schedulerCreateCmd.Flags())

	schedulerCmd.AddCommand(schedulerListCmd)
	schedulerCmd.AddCommand(schedulerCreateCmd)
	schedulerCmd.AddCommand(schedulerGetCmd)
	schedulerCmd.AddCommand(schedulerUpdateCmd)
	schedulerCmd.AddCommand(schedulerDeleteCmd)
	schedulerCmd.AddCommand(schedulerTriggerCmd)
	schedulerCmd.AddCommand(schedulerRunsCmd)
	rootCmd.AddCommand(schedulerCmd)
}
