package cli

import (
	"encoding/base64"
	"fmt"
	"io"
	"os"

	"github.com/auto-developer-orchestrator/backend/internal/cli/api"
	"github.com/spf13/cobra"
)

var filesCmd = &cobra.Command{
	Use:   "files",
	Short: "Sandbox file transfer commands",
}

var (
	fileSrc string
	fileDst string
	fileOut string
	filePath string
)

var filesUploadCmd = &cobra.Command{
	Use:   "upload <id>",
	Short: "Upload file to sandbox",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if fileSrc == "" || fileDst == "" {
			return fmt.Errorf("--src and --dst are required")
		}
		data, err := os.ReadFile(fileSrc)
		if err != nil {
			return fmt.Errorf("read local file: %w", err)
		}
		client := api.NewClient(serverURL)
		req := map[string]string{
			"path":    fileDst,
			"content": base64.StdEncoding.EncodeToString(data),
			"encoding": "base64",
		}
		if err := client.Post("/api/sandbox/"+args[0]+"/files/upload", req, nil); err != nil {
			return err
		}
		fmt.Printf("Uploaded %s → %s\n", fileSrc, fileDst)
		return nil
	},
}

var filesDownloadCmd = &cobra.Command{
	Use:   "download <id>",
	Short: "Download file from sandbox",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if filePath == "" {
			return fmt.Errorf("--path is required")
		}
		client := api.NewClient(serverURL)
		// Try raw format first
		resp, err := client.StreamGet("/api/sandbox/" + args[0] + "/files/download?path=" + filePath + "&format=raw")
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		if fileOut != "" {
			f, err := os.Create(fileOut)
			if err != nil {
				return err
			}
			defer f.Close()
			_, err = io.Copy(f, resp.Body)
			fmt.Printf("Downloaded %s → %s\n", filePath, fileOut)
			return err
		}
		_, err = io.Copy(os.Stdout, resp.Body)
		return err
	},
}

var filesListCmd = &cobra.Command{
	Use:   "list <id>",
	Short: "List files in sandbox",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := api.NewClient(serverURL)
		path := "/api/sandbox/" + args[0] + "/files/list"
		if filePath != "" {
			path += "?path=" + filePath
		}
		var result []map[string]interface{}
		if err := client.Get(path, &result); err != nil {
			return err
		}
		if outputFmt == "json" {
			return printJSON(result)
		}
		for _, f := range result {
			name, _ := f["name"].(string)
			isDir, _ := f["is_dir"].(bool)
			size, _ := f["size"].(float64)
			prefix := "  "
			if isDir {
				prefix = "📁 "
			}
			fmt.Printf("%s%-30s  %d\n", prefix, name, int64(size))
		}
		return nil
	},
}

func init() {
	filesUploadCmd.Flags().StringVar(&fileSrc, "src", "", "Local source path")
	filesUploadCmd.Flags().StringVar(&fileDst, "dst", "", "Remote destination path")
	filesDownloadCmd.Flags().StringVar(&filePath, "path", "", "Remote file path")
	filesDownloadCmd.Flags().StringVar(&fileOut, "out", "", "Local output path")
	filesListCmd.Flags().StringVar(&filePath, "path", "", "Directory path (default: /sandbox/workspace)")

	filesCmd.AddCommand(filesUploadCmd)
	filesCmd.AddCommand(filesDownloadCmd)
	filesCmd.AddCommand(filesListCmd)
	rootCmd.AddCommand(filesCmd)
}
