package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/auto-developer-orchestrator/backend/internal/agents/common"
	"github.com/auto-developer-orchestrator/backend/internal/skills"
	"github.com/spf13/cobra"
)

var skillsCmd = &cobra.Command{
	Use:   "skills",
	Short: "List and inspect discoverable skills",
	Long: `Inspect skills loaded from the kernel or an org's skills_dir.

Without --org, scans kernel skills only (config/capabilities/<name>/SKILL.md
files are capability SKILLs and not listed here; this lists the discoverable
skills the CTO reads via read_skill).

With --org X, also scans ~/.pux/orgs/X/skills/ (or legacy locations).`,
}

var skillsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List loaded skills (name, version, description)",
	RunE:  runSkillsList,
}

var skillsShowCmd = &cobra.Command{
	Use:   "show <name>",
	Short: "Print the full body of a single skill",
	Args:  cobra.ExactArgs(1),
	RunE:  runSkillsShow,
}

var skillsJSONCmd = &cobra.Command{
	Use:   "json",
	Short: "Dump all skills as JSON",
	RunE:  runSkillsJSON,
}

var (
	skillsOrgName string
)

func init() {
	skillsCmd.PersistentFlags().StringVar(&skillsOrgName, "org", "", "organization name (looks in ~/.pux/orgs/<name>)")
	skillsCmd.AddCommand(skillsListCmd)
	skillsCmd.AddCommand(skillsShowCmd)
	skillsCmd.AddCommand(skillsJSONCmd)
	rootCmd.AddCommand(skillsCmd)
}

// loadSkills resolves the kernel + org skills dirs (if --org set) and loads
// them through the production loader. Returns the store + the dirs it scanned
// so the caller can surface provenance.
func loadSkills() (*skills.Store, []string) {
	dirs := skillsDirs()
	store := skills.NewStore()
	store.LoadFromDirs(dirs)
	return store, dirs
}

// skillsDirs returns the dirs to scan, in order. Kernel skills come first;
// org skills overlay if --org is set and the dir exists.
func skillsDirs() []string {
	var dirs []string

	if root := os.Getenv("PROJECT_ROOT"); root != "" {
		dirs = append(dirs, filepath.Join(root, "config", "skills"))
	}
	if cfg := common.FindKernelConfigDir(); cfg != "" {
		dirs = append(dirs, filepath.Join(cfg, "skills"))
	}

	if skillsOrgName != "" {
		if orgPath, err := resolveOrgPath(skillsOrgName); err == nil {
			org := common.LoadOrgManifest(orgPath)
			if org != nil && org.SkillsDir != "" {
				dirs = append(dirs, org.SkillsDirPath())
			}
		}
	}

	return dirs
}

func runSkillsList(cmd *cobra.Command, args []string) error {
	store, dirs := loadSkills()

	if store.Count() == 0 {
		fmt.Fprintf(os.Stderr, "No skills found. Scanned: %s\n", strings.Join(dirs, ", "))
		if skillsOrgName == "" {
			fmt.Fprintln(os.Stderr, "Tip: pass --org <name> to also scan ~/.pux/orgs/<name>/skills/")
		}
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tVERSION\tCAPABILITIES\tDESCRIPTION")
	names := store.All()
	sort.Slice(names, func(i, j int) bool { return names[i].Name < names[j].Name })
	for _, s := range names {
		caps := strings.Join(s.Capabilities, ",")
		desc := s.Description
		if len(desc) > 70 {
			desc = desc[:67] + "..."
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", s.Name, s.Version, caps, desc)
	}
	return w.Flush()
}

func runSkillsShow(cmd *cobra.Command, args []string) error {
	store, _ := loadSkills()
	name := args[0]
	body := store.ReadSkill(name)
	if body == "" {
		return fmt.Errorf("skill %q not found", name)
	}
	fmt.Println(body)
	return nil
}

func runSkillsJSON(cmd *cobra.Command, args []string) error {
	store, _ := loadSkills()
	all := store.All()
	out := make([]map[string]any, 0, len(all))
	for _, s := range all {
		out = append(out, map[string]any{
			"name":         s.Name,
			"version":      s.Version,
			"description":  s.Description,
			"location":     s.Location,
			"capabilities": s.Capabilities,
		})
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
