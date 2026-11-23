package cli

// hl backup and hl restore drive an app's own `backup` service, as the backup contract
// (docs/backup-contract.md) describes: hl finds it by its compose labels and runs it with the
// same project name and override as `hl up`, so it acts on the app hl runs.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/DeM-Conve/project__hl/internal/compose"
	"github.com/DeM-Conve/project__hl/internal/config"
	"github.com/DeM-Conve/project__hl/internal/defaults"
	"github.com/DeM-Conve/project__hl/internal/interrupt"
)

var backupTag = regexp.MustCompile(defaults.BackupTagPattern)

// errNoContract is an app without the backup contract.
var errNoContract = errors.New("no backup contract")

// backupApp is an app that follows the contract, prepared to run.
type backupApp struct {
	*preparedApp
	app string // its name in hl.yml (the project is hl-<app>)
	// stop are the services a restore stops (the backup.stop label).
	stop []string
}

// contract reads the backup contract from an app's compose model: errNoContract when it has
// none, an error when it has one hl can't drive.
func contract(name string, m *compose.Model) (stop []string, err error) {
	s, ok := m.Services[defaults.BackupService]
	if !ok {
		return nil, errNoContract
	}
	v, ok := s.Labels[defaults.BackupContractLabel]
	if !ok {
		return nil, errNoContract
	}
	if v != defaults.BackupContractVersion {
		return nil, withHint(fmt.Errorf("%s: its %s service follows backup contract version %q; this hl knows version %s",
			name, defaults.BackupService, v, defaults.BackupContractVersion), "update hl, or use the app's own backup scripts")
	}
	for _, svc := range strings.Split(s.Labels[defaults.BackupStopLabel], ",") {
		if svc = strings.TrimSpace(svc); svc == "" {
			continue
		}
		if _, ok := m.Services[svc]; !ok || svc == defaults.BackupService {
			return nil, withHint(fmt.Errorf("%s: the %s label names %q, which isn't one of its other services (%s)",
				name, defaults.BackupStopLabel, svc, strings.Join(sortedServices(m.Services), ", ")),
				"the app's compose file must list the services that write its data; see %s", defaults.BackupContractURL)
		}
		if !slices.Contains(stop, svc) {
			stop = append(stop, svc)
		}
	}
	return stop, nil
}

// loadBackupApp checks that the app follows the contract and isn't another homelab's, then
// prepares it the way hl up does. An app without the contract is only read, never prepared.
func loadBackupApp(ctx context.Context, cfg *config.Config, inv *inventory, name string) (*backupApp, error) {
	p, err := project(cfg, name, cfg.Apps[name])
	if err != nil {
		return nil, err
	}
	m, err := p.Model(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	stop, err := contract(name, m)
	if err != nil {
		return nil, err
	}
	if err := inv.mine(cfg, cfg.ProjectName(name), name); err != nil {
		return nil, err
	}
	pa, err := prepare(ctx, cfg, name)
	if err != nil {
		return nil, err
	}
	return &backupApp{preparedApp: pa, app: name, stop: stop}, nil
}

// noContract is the error for an app named on the command line that has no contract.
func noContract(name string) error {
	return withHint(fmt.Errorf("%s has no backup contract: no %q service labelled %s=%s",
		name, defaults.BackupService, defaults.BackupContractLabel, defaults.BackupContractVersion),
		"hl backs up apps that follow %s; back up the others' data folders (data/%s) yourself", defaults.BackupContractURL, name)
}

// runBackup runs `backup <args>` in a one-off container of the app's backup service and
// returns what it printed on stdout; its messages go to hl's stderr as they come.
func runBackup(ctx context.Context, p *compose.Project, args ...string) (string, error) {
	// -T: no terminal, so stdout carries only the command's answer.
	a := append([]string{"run", "--rm", "--no-deps", "-T", defaults.BackupService, defaults.BackupCommand}, args...)
	cmd := p.Command(a...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, stderr
	err := compose.Exec(ctx, cmd)
	return out.String(), err
}

// backupExit is the exit code of a failed backup command, or 0.
func backupExit(err error) int {
	var ce *compose.Error
	if errors.As(err, &ce) && ce.Code > 0 {
		return ce.Code
	}
	return 0
}

// lastLine is the last non-empty line of s.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

func backupCmd() *cobra.Command {
	var tag string
	var list bool
	c := &cobra.Command{
		Use:   "backup [app...]",
		Short: "Back up apps that follow the backup contract (all of them by default); --list <app> lists an app's backups",
		Long: `Back up apps that follow the backup contract: their compose file has a
"backup" service labelled backup.contract=1, which hl runs as the app's own
"backup create". With no app, every app that has one; the others are listed
as skipped. hl stops nothing: the app's backup is consistent while it runs.

  hl backup                   back up every app that can
  hl backup my-app --tag v2   one app, tagged (retention never deletes it)
  hl backup --list my-app     the app's backups, newest first

The contract: ` + defaults.BackupContractURL,
		ValidArgsFunction: completeApps,
		RunE: func(cmd *cobra.Command, args []string) error {
			if list && len(args) != 1 {
				return &usageError{err: errors.New("--list takes one app"), cmd: "hl backup"}
			}
			if list && tag != "" {
				return &usageError{err: errors.New("--tag makes a backup; --list only lists them"), cmd: "hl backup"}
			}
			if tag != "" && (!backupTag.MatchString(tag) || tag == defaults.BackupReservedTag) {
				return &usageError{err: withHint(fmt.Errorf("invalid tag %q", tag),
					"a tag is lowercase letters, digits and dashes, and not %q (the safety backup's own)", defaults.BackupReservedTag)}
			}
			cfg, err := load()
			if err != nil {
				return err
			}
			names, err := targets(cfg, args)
			if err != nil {
				return err
			}
			if list {
				return listBackups(cmd.Context(), cfg, names[0])
			}
			return backupApps(cmd.Context(), cfg, names, tag, len(args) == 0)
		},
	}
	c.Flags().StringVar(&tag, "tag", "", "tag the backups (lowercase letters, digits and dashes); retention never deletes a tagged one")
	c.Flags().BoolVar(&list, "list", false, "list the app's backups instead of making one")
	return c
}

// backupApps runs `backup create` for each app; with all, apps without the contract are
// skipped and listed instead of failing.
func backupApps(ctx context.Context, cfg *config.Config, names []string, tag string, all bool) error {
	inv, err := containers(ctx)
	if err != nil {
		return err
	}
	var apps []*backupApp
	var skipped, failed []string
	var lastErr error
	for _, n := range names {
		ba, err := loadBackupApp(ctx, cfg, inv, n)
		switch {
		case errors.Is(err, errNoContract) && all:
			skipped = append(skipped, n)
		case errors.Is(err, errNoContract):
			if len(names) > 1 {
				printErr(noContract(n))
			}
			failed, lastErr = append(failed, n), noContract(n)
		case errors.Is(err, interrupt.ErrInterrupted):
			return err
		case err != nil:
			if len(names) > 1 {
				printErr(err)
			}
			failed, lastErr = append(failed, n), err
		default:
			apps = append(apps, ba)
		}
	}
	if all && len(apps) == 0 && len(failed) == 0 {
		return withHint(errors.New("no app follows the backup contract"),
			"hl backs up apps whose compose file has a %q service labelled %s=%s (%s)",
			defaults.BackupService, defaults.BackupContractLabel, defaults.BackupContractVersion, defaults.BackupContractURL)
	}
	var made []string
	for _, ba := range apps {
		step(ba.name())
		args := []string{"create"}
		if tag != "" {
			args = append(args, "--tag", tag)
		}
		out, err := runBackup(ctx, ba.Project, args...)
		if errors.Is(err, interrupt.ErrInterrupted) {
			return err
		}
		if err != nil || strings.TrimSpace(out) == "" {
			if err == nil {
				err = errors.New("the backup command named no archive")
			}
			err = withHint(fmt.Errorf("%s: backup failed: %w", ba.name(), err), "its messages are above; the backup service's logs: `hl logs %s %s`", ba.name(), defaults.BackupService)
			if len(names) > 1 {
				printErr(err)
			}
			failed, lastErr = append(failed, ba.name()), err
			continue
		}
		say("%s%s%s", green, lastLine(out), reset)
		made = append(made, ba.name())
	}
	if len(names) > 1 {
		fmt.Fprintln(stdout)
		if len(made) > 0 {
			say("backed up: %s", strings.Join(made, ", "))
		}
		if len(skipped) > 0 {
			say("%sskipped, no backup contract:%s %s", dim, reset, strings.Join(skipped, ", "))
		}
	}
	switch {
	case len(failed) == 0:
		return nil
	case len(names) == 1:
		return lastErr
	}
	return fmt.Errorf("%d of %d apps failed: %s", len(failed), len(names)-len(skipped), strings.Join(failed, ", "))
}

// name is the app's name in hl.yml.
func (ba *backupApp) name() string { return ba.app }

// archive is one line of `backup list`.
type archive struct {
	name, size, created, tag string
}

// archives parses `backup list`: name, size in bytes, creation time and tag, tab-separated.
func archives(out string) []archive {
	var as []archive
	for _, l := range strings.Split(out, "\n") {
		if strings.TrimSpace(l) == "" {
			continue
		}
		f := strings.Split(strings.TrimRight(l, "\r"), "\t")
		for len(f) < 4 {
			f = append(f, "")
		}
		as = append(as, archive{name: f[0], size: f[1], created: f[2], tag: f[3]})
	}
	return as
}

func listBackups(ctx context.Context, cfg *config.Config, name string) error {
	inv, err := containers(ctx)
	if err != nil {
		return err
	}
	ba, err := loadBackupApp(ctx, cfg, inv, name)
	if errors.Is(err, errNoContract) {
		return noContract(name)
	}
	if err != nil {
		return err
	}
	out, err := runBackup(ctx, ba.Project, "list")
	if err != nil {
		return fmt.Errorf("%s: listing the backups failed: %w", name, err)
	}
	as := archives(out)
	if len(as) == 0 {
		say("%s has no backups yet - make one with: hl backup %s", name, name)
		return nil
	}
	w := table()
	fmt.Fprintln(w, "NAME\tSIZE\tCREATED\tTAG")
	for _, a := range as {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", a.name, humanSize(a.size), a.created, a.tag)
	}
	return w.Flush()
}

// humanSize writes a size in bytes as 1.2 MB; anything else as it is.
func humanSize(s string) string {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 {
		return s
	}
	const unit = 1000
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	v, units := float64(n)/unit, "kMGTPE"
	i := 0
	for v >= unit && i < len(units)-1 {
		v /= unit
		i++
	}
	return fmt.Sprintf("%.1f %cB", v, units[i])
}

func restoreCmd() *cobra.Command {
	var yes bool
	c := &cobra.Command{
		Use:   "restore <app> <backup|latest>",
		Short: "Restore an app from one of its backups (backup contract): verify, confirm, stop, restore, start",
		Long: `Restore an app that follows the backup contract from one of its backups
(` + "`hl backup --list <app>`" + ` names them; latest is the newest):

  1. verify the archive; nothing changes if it is bad
  2. ask (--yes skips the question; without a terminal, --yes is required)
  3. stop the services its backup.stop label names (its database keeps running)
  4. restore: the app's backup takes a pre-restore safety backup first and puts
     the old data back if the restore fails
  5. start those services again and wait until they are healthy

The app must be running (its database is needed). The contract: ` + defaults.BackupContractURL,
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(2)(cmd, args); err != nil {
				return err
			}
			if args[1] == "" || strings.HasPrefix(args[1], "-") {
				return fmt.Errorf("invalid backup name %q", args[1])
			}
			return nil
		},
		ValidArgsFunction: completeApp,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := load()
			if err != nil {
				return err
			}
			if _, err := targets(cfg, args[:1]); err != nil {
				return err
			}
			if !yes && !stdinTerminal(stdin) {
				return withHint(fmt.Errorf("not a terminal, so hl can't ask before replacing %s's data; nothing changed", args[0]),
					"pass --yes to restore without asking")
			}
			return restore(cmd.Context(), cfg, args[0], args[1], yes)
		},
	}
	c.Flags().BoolVar(&yes, "yes", false, "don't ask before replacing the app's data")
	return c
}

func restore(ctx context.Context, cfg *config.Config, name, backup string, yes bool) error {
	inv, err := containers(ctx)
	if err != nil {
		return err
	}
	ba, err := loadBackupApp(ctx, cfg, inv, name)
	if errors.Is(err, errNoContract) {
		return noContract(name)
	}
	if err != nil {
		return err
	}
	if len(ba.stop) == 0 {
		return withHint(fmt.Errorf("%s: its %s service has no %s label, so hl doesn't know which services to stop during a restore",
			name, defaults.BackupService, defaults.BackupStopLabel), "the app's compose file must name them; see %s", defaults.BackupContractURL)
	}
	if !inv.running(ba.Name) {
		return withHint(fmt.Errorf("%s isn't running; a restore needs it up (its database stays up while the data is replaced)", name),
			"start it with `hl up %s`, then restore", name)
	}
	p := ba.Project
	step(name)
	if backup == defaults.BackupLatest {
		out, err := runBackup(ctx, p, "list")
		if err != nil {
			return fmt.Errorf("%s: listing the backups failed: %w", name, err)
		}
		as := archives(out)
		if len(as) == 0 {
			return withHint(fmt.Errorf("%s has no backups", name), "make one with `hl backup %s`", name)
		}
		backup = as[0].name
	}

	// 1. Verify: a bad archive stops here, with nothing changed.
	say("%sverifying %s%s", dim, backup, reset)
	if _, err := runBackup(ctx, p, "verify", backup); err != nil {
		if errors.Is(err, interrupt.ErrInterrupted) {
			return err
		}
		if backupExit(err) == defaults.BackupExitVerifyFailed {
			return withHint(fmt.Errorf("%s: %s failed verification; nothing changed", name, backup), "pick another: `hl backup --list %s`", name)
		}
		return withHint(fmt.Errorf("%s: verifying %s failed: %w; nothing changed", name, backup, err), "`hl backup --list %s` names the backups", name)
	}

	// 2. Confirm.
	if !yes && !newPrompter(stdin, stdout).confirm(fmt.Sprintf("Replace %s's data with %s? This stops %s while it restores",
		name, backup, strings.Join(ba.stop, ", ")), false) {
		say("nothing changed")
		return nil
	}

	// 3. Stop the services that write the data; the database keeps running.
	if err := p.Run(ctx, append([]string{"stop"}, ba.stop...)...); err != nil {
		if errors.Is(err, interrupt.ErrInterrupted) {
			return err
		}
		return withHint(fmt.Errorf("%s: stopping %s failed: %w; nothing restored", name, strings.Join(ba.stop, ", "), err),
			"`hl up %s` starts whatever stopped", name)
	}

	// 4. Restore. The backup takes a pre-restore safety backup and rolls back on failure.
	out, rerr := runBackup(ctx, p, "restore", backup)
	if errors.Is(rerr, interrupt.ErrInterrupted) {
		return withHint(rerr, "%s stay stopped: once the restore has ended, check `hl backup --list %s` and start them with `hl up %s`",
			strings.Join(ba.stop, ", "), name, name)
	}

	// 5. Start them again, whatever happened, and wait until they are healthy.
	say("%sstarting %s%s", dim, strings.Join(ba.stop, ", "), reset)
	serr := p.Run(ctx, append([]string{"start"}, ba.stop...)...)
	if serr == nil {
		serr = waitHealthy(ctx, ba, ba.stop)
	} else if !errors.Is(serr, interrupt.ErrInterrupted) {
		serr = withHint(fmt.Errorf("%s: starting %s failed: %w", name, strings.Join(ba.stop, ", "), serr), "start it with `hl up %s`", name)
	}

	switch code := backupExit(rerr); {
	case rerr == nil:
		say("%srestored %s from %s%s", green, name, backup, reset)
		if safety := lastLine(out); safety != "" {
			say("the data from before is in %s", safety)
		}
		return serr
	case code == defaults.BackupExitRolledBack:
		rerr = withHint(fmt.Errorf("%s: the restore failed and was rolled back; the old data is in place", name), "its messages are above")
	case code == defaults.BackupExitVerifyFailed:
		rerr = withHint(fmt.Errorf("%s: %s failed verification; nothing changed", name, backup), "pick another: `hl backup --list %s`", name)
	default:
		rerr = withHint(fmt.Errorf("%s: the restore failed: %w", name, rerr),
			"its messages are above; the safety backup it took first (tag %s) is in `hl backup --list %s`", defaults.BackupReservedTag, name)
	}
	if serr != nil {
		printErr(serr)
	}
	return rerr
}

// waitHealthy waits until services run and are healthy (or have no healthcheck), up to
// waitTimeout; one that doesn't gets its last log lines shown.
func waitHealthy(ctx context.Context, ba *backupApp, services []string) error {
	deadline := time.Now().Add(waitTimeout)
	say("%swaiting for %s%s", dim, strings.Join(services, ", "), reset)
	for {
		inv, err := containers(ctx)
		if err != nil {
			return err
		}
		ready, bad := true, false
		for _, s := range services {
			n := 0
			for _, c := range inv.of(ba.Name) {
				if c.service != s {
					continue
				}
				n++
				switch h := health(c); {
				case c.state != "running" || h == "starting":
					ready = false
				case h == "unhealthy":
					ready, bad = false, true
				}
			}
			if n == 0 {
				ready = false
			}
		}
		if ready {
			return nil
		}
		if bad || time.Now().After(deadline) {
			return unhealthy(ctx, ba.Project, ba.name(), errors.New("not healthy after the restore"), services)
		}
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case <-time.After(defaults.HealthPollInterval):
		}
	}
}
