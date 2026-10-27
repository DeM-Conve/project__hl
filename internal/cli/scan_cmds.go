package cli

// hl projects, hl ignore and hl strip-prefix: the hl.yml lists that tell hl init and hl sync
// where apps are and how to name them, changed from the command line (comments kept).

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/DeM-Conve/project__hl/internal/config"
	"github.com/DeM-Conve/project__hl/internal/defaults"
)

// listSpec describes one hl.yml list and its command.
type listSpec struct {
	key   string // hl.yml key
	use   string // command name
	short string
	long  string
	what  string // one entry, for messages: "folder", "pattern", "prefix"
	// get reads the list from a loaded config.
	get func(cfg *config.Config) []string
	// norm turns an argument into the entry as it goes into hl.yml (nil: as it is).
	norm func(cfg *config.Config, arg string) (string, error)
	// same reports whether entry (from hl.yml) is the argument (nil: equal strings).
	same func(cfg *config.Config, entry, arg string) bool
	// show prints the list (nil: one entry a line).
	show func(cfg *config.Config)
	// next is said after a change.
	next string
}

func projectsCmd() *cobra.Command {
	return listCmd(listSpec{
		key: "projects", use: "projects", what: "folder",
		short: "List, add or remove projects folders: where hl init and hl sync find apps",
		long: `The projects folders (projects: in hl.yml) hold your apps: each subfolder with a
compose file is one. hl sync adds new ones and removes apps whose folder is gone.

  hl projects                  list them, and whether they exist
  hl projects add ~/code       add a folder (it must exist)
  hl projects remove ~/code    remove one; its apps stay in hl.yml`,
		get: func(cfg *config.Config) []string { return cfg.Projects },
		norm: func(cfg *config.Config, arg string) (string, error) {
			abs, err := filepath.Abs(config.ExpandHome(arg))
			if err != nil {
				return "", err
			}
			if st, err := os.Stat(abs); err != nil || !st.IsDir() {
				return "", withHint(fmt.Errorf("folder %s not found", abs), "pass a folder that exists")
			}
			return storedPath(cfg.Dir, abs), nil
		},
		same: func(cfg *config.Config, entry, arg string) bool {
			if entry == arg {
				return true
			}
			abs, err := filepath.Abs(config.ExpandHome(arg))
			return err == nil && cfg.Abs(entry) == abs
		},
		show: func(cfg *config.Config) {
			for i, p := range cfg.Projects {
				dir := cfg.ProjectDirs()[i]
				if st, err := os.Stat(dir); err != nil || !st.IsDir() {
					say("%s  %s(not found: %s)%s", p, yellow, dir, reset)
				} else {
					say("%s", p)
				}
			}
		},
		next: "run hl sync to add or remove apps to match",
	})
}

func ignoreCmd() *cobra.Command {
	return listCmd(listSpec{
		key: "ignore", use: "ignore", what: "pattern",
		short: "List, add or remove folder-name patterns hl init and hl sync skip",
		long: `Folder names matching these patterns (ignore: in hl.yml; *, ? and [...]) are
never added by hl init or hl sync. Always skipped too: ` + strings.Join(defaults.DefaultIgnore, ", ") + `,
and folders holding a ` + defaults.IgnoreFile + ` file. An app already in hl.yml stays.

  hl ignore                    list the patterns
  hl ignore add old 'scratch-*'
  hl ignore remove old`,
		get: func(cfg *config.Config) []string { return cfg.Ignore },
		norm: func(_ *config.Config, arg string) (string, error) {
			if _, err := filepath.Match(arg, ""); err != nil {
				return "", withHint(fmt.Errorf("%q is not a valid pattern", arg), "use *, ? and [...], e.g. 'scratch-*'")
			}
			return arg, nil
		},
		show: func(cfg *config.Config) {
			for _, p := range cfg.Ignore {
				say("%s", p)
			}
			say("%salways: %s; folders with a %s file%s", dim, strings.Join(defaults.DefaultIgnore, ", "), defaults.IgnoreFile, reset)
		},
		next: "hl sync no longer offers matching folders",
	})
}

func stripPrefixCmd() *cobra.Command {
	return listCmd(listSpec{
		key: "strip_prefix", use: "strip-prefix", what: "prefix",
		short: "List, add or remove folder-name prefixes left out of app names",
		long: `hl init, hl sync and hl add (without --name) name an app after its folder, less
the first of these prefixes it starts with (strip_prefix: in hl.yml): with
project__, folder project__blog becomes app blog. Apps already added keep their names.

  hl strip-prefix              list the prefixes
  hl strip-prefix add project__
  hl strip-prefix remove project__`,
		get:  func(cfg *config.Config) []string { return cfg.StripPrefix },
		next: "new apps are named without it",
	})
}

// listCmd is `hl <use> [list]`, `hl <use> add <entry>...` and `hl <use> remove <entry>...`
// for one hl.yml list.
func listCmd(s listSpec) *cobra.Command {
	list := func(cmd *cobra.Command, args []string) error {
		cfg, err := load()
		if err != nil {
			return err
		}
		if len(s.get(cfg)) == 0 {
			say("no %s in %s - add one with hl %s add <%s>", s.key, config.FileName, s.use, s.what)
		}
		if s.show != nil {
			s.show(cfg)
			return nil
		}
		for _, e := range s.get(cfg) {
			say("%s", e)
		}
		return nil
	}
	c := &cobra.Command{
		Use:   s.use,
		Short: s.short,
		Long:  s.long,
		Args:  cobra.NoArgs,
		RunE:  list,
	}
	completeEntries := func(cmd *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
		cfg, err := load()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return without(s.get(cfg), args), cobra.ShellCompDirectiveNoFileComp
	}
	add := &cobra.Command{
		Use:   "add <" + s.what + ">...",
		Short: "Add to " + s.key + " in hl.yml",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := load()
			if err != nil {
				return err
			}
			entries := append([]string(nil), s.get(cfg)...)
			var added []string
			for _, arg := range args {
				e := arg
				if s.norm != nil {
					if e, err = s.norm(cfg, arg); err != nil {
						return err
					}
				}
				if containsEntry(cfg, s, entries, e) || containsEntry(cfg, s, entries, arg) {
					say("%s is already in %s", e, s.key)
					continue
				}
				entries = append(entries, e)
				added = append(added, e)
			}
			if len(added) == 0 {
				return nil
			}
			if err := saveList(cfg, s.key, entries); err != nil {
				return err
			}
			say("added to %s: %s", s.key, strings.Join(added, ", "))
			if s.next != "" {
				say("%s", s.next)
			}
			return nil
		},
	}
	if s.key == "projects" {
		add.ValidArgsFunction = func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
			return nil, cobra.ShellCompDirectiveFilterDirs
		}
	}
	remove := &cobra.Command{
		Use:               "remove <" + s.what + ">...",
		Aliases:           []string{"rm"},
		Short:             "Remove from " + s.key + " in hl.yml",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: completeEntries,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := load()
			if err != nil {
				return err
			}
			entries := s.get(cfg)
			drop := map[int]bool{}
			for _, arg := range args {
				i := -1
				for j, e := range entries {
					if e == arg || (s.same != nil && s.same(cfg, e, arg)) {
						i = j
						break
					}
				}
				if i < 0 {
					hint := "it has none"
					if len(entries) > 0 {
						hint = s.key + ": " + strings.Join(entries, ", ")
					}
					return withHint(fmt.Errorf("%s is not in %s", arg, s.key), "%s", hint)
				}
				drop[i] = true
			}
			var kept, removed []string
			for i, e := range entries {
				if drop[i] {
					removed = append(removed, e)
				} else {
					kept = append(kept, e)
				}
			}
			if err := saveList(cfg, s.key, kept); err != nil {
				return err
			}
			say("removed from %s: %s", s.key, strings.Join(removed, ", "))
			if s.key == "projects" {
				say("its apps stay in %s; hl sync no longer looks there (hl remove <app> takes one out)", config.FileName)
			}
			return nil
		},
	}
	listSub := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List " + s.key,
		Args:    cobra.NoArgs,
		RunE:    list,
	}
	c.AddCommand(listSub, add, remove)
	return c
}

// containsEntry reports whether arg names an entry already in the list, in any spelling s.same accepts.
func containsEntry(cfg *config.Config, s listSpec, entries []string, arg string) bool {
	for _, e := range entries {
		if e == arg || (s.same != nil && s.same(cfg, e, arg)) {
			return true
		}
	}
	return false
}

func saveList(cfg *config.Config, key string, entries []string) error {
	if err := upgradeConfig(cfg.Dir); err != nil {
		return err
	}
	return config.SetList(cfg.Dir, key, entries)
}
