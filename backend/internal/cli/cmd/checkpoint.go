package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/checkpoint"
	"github.com/auto-developer-orchestrator/backend/internal/cli/api"
	"github.com/spf13/cobra"
)

var checkpointCmd = &cobra.Command{
	Use:   "checkpoint",
	Short: "Manage file checkpoints",
}

var (
	checkpointSession string
	checkpointPath    string
)

func init() {
	checkpointCmd.PersistentFlags().StringVarP(&checkpointSession, "session", "s", "", "Session ID")
	checkpointCmd.PersistentFlags().StringVar(&checkpointPath, "path", "", "File path (for file-history/file-restore)")

	checkpointCmd.AddCommand(checkpointListCmd)
	checkpointCmd.AddCommand(checkpointRestoreCmd)
	checkpointCmd.AddCommand(checkpointDiffCmd)
	checkpointCmd.AddCommand(checkpointFileHistoryCmd)
	rootCmd.AddCommand(checkpointCmd)
}

var checkpointListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all checkpoints for a session",
	RunE: func(cmd *cobra.Command, args []string) error {
		session := resolveCheckpointSession()
		if session == "" {
			return fmt.Errorf("specify --session or --project")
		}

		if serverURL != "" {
			// Use API
			client := api.NewClient(serverURL)
			var snapshots []*checkpoint.Snapshot
			if err := client.Get("/api/pux/checkpoints?session="+session, &snapshots); err != nil {
				return err
			}
			if outputFmt == "json" {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(snapshots)
			}
			if len(snapshots) == 0 {
				fmt.Println("No checkpoints found.")
				return nil
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tTIMESTAMP\tLABEL\tFILES\tROUND")
			for _, s := range snapshots {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%d\n",
					s.ID, s.Timestamp.Format(time.Stamp), s.Label, s.FileCount, s.Round)
			}
			tw.Flush()
			return nil
		}

		// Direct disk access (no server)
		return listCheckpointsDisk(session)
	},
}

var checkpointRestoreCmd = &cobra.Command{
	Use:   "restore <snapshot-id>",
	Short: "Restore files to a snapshot",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		session := resolveCheckpointSession()
		if session == "" {
			return fmt.Errorf("specify --session or --project")
		}
		snapID := args[0]

		if serverURL != "" {
			client := api.NewClient(serverURL)
			var result map[string]any
			if err := client.Post("/api/pux/checkpoints/"+snapID+"/restore",
				map[string]string{"session": session}, &result); err != nil {
				return err
			}
			if outputFmt == "json" {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			}
			fmt.Printf("Restored %v files\n", result["count"])
			if files, ok := result["restored"].([]any); ok {
				for _, f := range files {
					fmt.Printf("  - %s\n", f)
				}
			}
			return nil
		}

		return restoreCheckpointDisk(session, snapID)
	},
}

var checkpointDiffCmd = &cobra.Command{
	Use:   "diff <snapshot-id>",
	Short: "Show files that differ from a snapshot",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		session := resolveCheckpointSession()
		if session == "" {
			return fmt.Errorf("specify --session or --project")
		}
		snapID := args[0]
		return diffCheckpointDisk(session, snapID)
	},
}

var checkpointFileHistoryCmd = &cobra.Command{
	Use:   "file-history",
	Short: "List all versions of a file",
	RunE: func(cmd *cobra.Command, args []string) error {
		session := resolveCheckpointSession()
		if session == "" {
			return fmt.Errorf("specify --session or --project")
		}
		if checkpointPath == "" {
			return fmt.Errorf("specify --path")
		}
		return fileHistoryDisk(session, checkpointPath)
	},
}

// resolveCheckpointSession resolves the session ID from --session, --project, or --org flags.
func resolveCheckpointSession() string {
	if checkpointSession != "" {
		return checkpointSession
	}
	if projectName != "" {
		return projectName
	}
	return ""
}

// --- Direct disk access (no server needed) ---

func checkpointBaseDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".pi", "agent", "checkpoints")
}

func loadManifestDisk(sessionID string) (*checkpoint.Manifest, error) {
	data, err := os.ReadFile(filepath.Join(checkpointBaseDir(), sessionID, "manifest.json"))
	if err != nil {
		return nil, err
	}
	var man checkpoint.Manifest
	if err := json.Unmarshal(data, &man); err != nil {
		return nil, err
	}
	return &man, nil
}

func listCheckpointsDisk(sessionID string) error {
	man, err := loadManifestDisk(sessionID)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("No checkpoints found.")
			return nil
		}
		return err
	}
	if len(man.Snapshots) == 0 {
		fmt.Println("No checkpoints found.")
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tTIMESTAMP\tLABEL\tFILES\tROUND")
	for _, s := range man.Snapshots {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%d\n",
			s.ID, s.Timestamp.Format(time.Stamp), s.Label, s.FileCount, s.Round)
	}
	tw.Flush()
	return nil
}

func restoreCheckpointDisk(sessionID, snapID string) error {
	man, err := loadManifestDisk(sessionID)
	if err != nil {
		return err
	}
	mgr := checkpoint.NewManager(sessionID, man.Project, filepath.Join(checkpointBaseDir(), sessionID))
	mgr.Load()
	restored, err := mgr.RestoreSnapshot(nil, snapID)
	if err != nil {
		return err
	}
	fmt.Printf("Restored %d files:\n", len(restored))
	for _, f := range restored {
		fmt.Printf("  - %s\n", f)
	}
	return nil
}

func diffCheckpointDisk(sessionID, snapID string) error {
	man, err := loadManifestDisk(sessionID)
	if err != nil {
		return err
	}
	for _, snap := range man.Snapshots {
		if snap.ID == snapID {
			fmt.Printf("Snapshot: %s (%s)\n", snap.ID, snap.Label)
			fmt.Printf("Files: %d\n", len(snap.Files))
			for path, fv := range snap.Files {
				fmt.Printf("  %s  v%d  %s  %d bytes\n", path, fv.Version, fv.Hash[:8], fv.Size)
			}
			return nil
		}
	}
	return fmt.Errorf("snapshot %s not found", snapID)
}

func fileHistoryDisk(sessionID, filePath string) error {
	man, err := loadManifestDisk(sessionID)
	if err != nil {
		return err
	}
	mgr := checkpoint.NewManager(sessionID, man.Project, filepath.Join(checkpointBaseDir(), sessionID))
	mgr.Load()
	versions := mgr.ListFileVersions(filePath)
	if len(versions) == 0 {
		fmt.Printf("No versions for %s\n", filePath)
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "VERSION\tHASH\tSIZE\tTIMESTAMP")
	for _, fv := range versions {
		fmt.Fprintf(tw, "v%d\t%s\t%d\t%s\n",
			fv.Version, fv.Hash[:8], fv.Size, fv.Timestamp.Format(time.Stamp))
	}
	tw.Flush()
	return nil
}
