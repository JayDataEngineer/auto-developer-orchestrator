package checkpoint

import "regexp"

// destructivePatterns matches bash commands that could modify or delete project files.
var destructivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`\brm\s`),          // rm, rm -rf, rm file
	regexp.MustCompile(`\brm$`),            // bare rm at end
	regexp.MustCompile(`\bmv\s`),           // mv (rename/move)
	regexp.MustCompile(`>`),                // redirect overwrite (echo "x" > file)
	regexp.MustCompile(`\bdd\s`),           // dd (raw write)
	regexp.MustCompile(`\bsed\s.*-i`),      // sed in-place
	regexp.MustCompile(`\btruncate\s`),     // truncate
	regexp.MustCompile(`\btee\s`),          // tee (overwrites)
	regexp.MustCompile(`\bgit\s+(checkout|reset|stash|rebase|clean)`), // git destructive ops
	regexp.MustCompile(`\bchmod\s`),        // permission change
	regexp.MustCompile(`\bchown\s`),        // ownership change
	regexp.MustCompile(`\binstall\s`),      // install command
	regexp.MustCompile(`\bcp\s`),           // cp (can overwrite)
	regexp.MustCompile(`\btar\s.*-x`),      // tar extract (can overwrite)
	regexp.MustCompile(`\bunzip\s`),        // unzip (can overwrite)
}

// IsDestructiveCommand returns true if the command might modify project files.
func IsDestructiveCommand(cmd string) bool {
	for _, p := range destructivePatterns {
		if p.MatchString(cmd) {
			return true
		}
	}
	return false
}
