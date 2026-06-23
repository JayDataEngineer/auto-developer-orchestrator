package cli

import (
	"bytes"
	"io"
	"os"
	"testing"
)

// runCommand executes the root cobra command with the given args,
// capturing stdout and stderr. The --server flag is automatically prepended.
// Global flags are reset before each invocation to prevent state leakage.
func runCommand(t *testing.T, serverURL string, args ...string) (stdout, stderr string, err error) {
	t.Helper()

	// Reset global flags to defaults
	outputFmt = "text"
	projectName = ""

	// Reset local command flags
	agentModel = ""
	agentThinking = ""
	agentAutoBranch = false
	schedName = ""
	schedProject = ""
	schedMessage = ""
	schedType = "manual"
	schedCron = ""
	schedEvery = ""
	schedAt = ""
	schedEnabled = true
	schedAutoBranch = false
	schedThinking = ""
	schedModel = ""
	schedWebhook = false
	sandboxID = ""
	sandboxProjPath = ""
	screenshotSave = ""
	fileSrc = ""
	fileDst = ""
	fileOut = ""
	filePath = ""
	checkoutBranch = ""
	githubToken = ""
	githubOwner = ""
	githubRepo = ""
	desktopAction = ""
	desktopElement = ""
	desktopText = ""
	desktopURL = ""
	desktopKey = ""
	desktopX = 0
	desktopY = 0
	desktopButton = "left"
	desktopDirection = ""
	desktopSubmit = false
	configSetArgs = nil
	skillsOrgName = ""

	fullArgs := append([]string{"--server", serverURL}, args...)

	// Capture stdout
	oldStdout := os.Stdout
	rOut, wOut, _ := os.Pipe()
	os.Stdout = wOut

	// Capture stderr
	oldStderr := os.Stderr
	rErr, wErr, _ := os.Pipe()
	os.Stderr = wErr

	rootCmd.SetArgs(fullArgs)
	rootCmd.SilenceErrors = true
	rootCmd.SilenceUsage = true

	err = rootCmd.Execute()

	wOut.Close()
	wErr.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr

	var bufOut, bufErr bytes.Buffer
	io.Copy(&bufOut, rOut)
	io.Copy(&bufErr, rErr)

	return bufOut.String(), bufErr.String(), err
}

// mustRunCommand is like runCommand but fails the test on error.
func mustRunCommand(t *testing.T, serverURL string, args ...string) string {
	t.Helper()
	stdout, stderr, err := runCommand(t, serverURL, args...)
	if err != nil {
		t.Fatalf("command %v failed: %v\nstderr: %s", args, err, stderr)
	}
	return stdout
}
