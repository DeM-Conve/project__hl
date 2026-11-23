package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Made-up apps: notes follows the backup contract (a web service, a database and a backup
// service that stops the web service during a restore); static has no backup service.
const (
	notesModel = `{"services":{"web":{"ports":[{"target":80}],"depends_on":{"db":{}}},"db":{},` +
		`"backup":{"labels":{"backup.contract":"1","backup.stop":"web"}}}}`
	staticModel = `{"services":{"web":{"ports":[{"target":80}]}}}`
)

const (
	runBackupArgs = "run --rm --no-deps -T backup backup"
	newest        = "notes-20261123-030000.tar.gz"
	older         = "notes-v2-20261122-030000.tar.gz"
)

// backupLab is a homelab with notes and static; Docker lists these containers (default: notes
// running and healthy).
func backupLab(t *testing.T, containers ...string) (f *fake, lab string) {
	t.Helper()
	f = newFake(t)
	notes := f.app("notes", notesModel)
	static := f.app("static", staticModel)
	lab = f.lab("lab", "apps:\n  notes: {path: '"+notes+"'}\n  static: {path: '"+static+"'}\n")
	if containers == nil {
		override := filepath.Join(lab, ".hl", "notes", "override.yml")
		containers = []string{
			ctr("hl-notes", "web", "running", "Up 1 minute (healthy)", override),
			ctr("hl-notes", "db", "running", "Up 1 minute", override),
		}
	}
	f.containers(containers...)
	return f, lab
}

// hlInput runs hl with in as its stdin, on a terminal.
func (f *fake) hlInput(in string, args ...string) result {
	f.t.Helper()
	old := stdinTerminal
	stdinTerminal = func(io.Reader) bool { return true }
	defer func() { stdinTerminal = old }()
	var out, errOut bytes.Buffer
	stdin, stdout, stderr = strings.NewReader(in), &out, &errOut
	code := runCLI(context.Background(), args)
	return result{code, out.String(), errOut.String()}
}

func TestBackupAll(t *testing.T) {
	f, lab := backupLab(t)
	f.on(rule{Match: []string{runBackupArgs + " create"}, Stdout: "progress on stdout is allowed\n" + newest + "\n", Stderr: "dumping the database\n"})
	r := f.must("backup")
	contains(t, "backup", r.out, "==> notes", newest, "backed up: notes", "skipped, no backup contract: static")
	contains(t, "backup's messages", r.errOut, "dumping the database")
	creates := f.find(runBackupArgs + " create")
	if len(creates) != 1 {
		t.Fatalf("want one backup create: %v", creates)
	}
	contains(t, "create", creates[0].String(), "--project-name hl-notes",
		"--file "+filepath.Join(lab, ".hl", "notes", "override.yml"))
	if creates[0].Env["HL_DATA"] != filepath.Join(lab, "data", "notes") {
		t.Errorf("not run with the app's env: %v", creates[0].Env)
	}
	// An app without the contract is only read: nothing is generated for it.
	if _, err := os.Stat(filepath.Join(lab, ".hl", "static")); err == nil {
		t.Errorf("hl backup prepared an app without the contract")
	}

	// A tag goes to backup create.
	f.reset()
	f.must("backup", "notes", "--tag", "before-upgrade")
	if len(f.find(runBackupArgs+" create --tag before-upgrade")) != 1 {
		t.Errorf("tag not passed: %v", f.calls())
	}
}

func TestBackupRefusals(t *testing.T) {
	f, _ := backupLab(t)
	for _, args := range [][]string{
		{"backup", "--tag", "Not OK"},
		{"backup", "--tag", "pre-restore"},
		{"backup", "--list"},
		{"backup", "--list", "notes", "static"},
		{"backup", "--list", "notes", "--tag", "x"},
		{"backup", "nope"},
		{"backup", "proxy"},
		{"restore", "notes"},
		{"restore", "nope", "latest", "--yes"},
	} {
		if r := f.hl(args...); r.code != 2 {
			t.Errorf("hl %v: want a usage error: %s", args, r)
		}
	}
	if runs := f.find("run --rm"); len(runs) > 0 {
		t.Errorf("usage errors ran the backup: %v", runs)
	}

	// An app named on the command line without the contract fails, with the way out.
	r := f.hl("backup", "static")
	if r.code != 1 {
		t.Fatalf("want a failure: %s", r)
	}
	contains(t, "no contract", r.errOut, "static has no backup contract", "backup.contract=1", "hint:", "backup-contract.md")
	if r := f.hl("restore", "static", "latest", "--yes"); r.code != 1 || !strings.Contains(r.errOut, "no backup contract") {
		t.Errorf("restore without the contract: %s", r)
	}

	// A failing backup create fails hl backup.
	f.on(rule{Match: []string{runBackupArgs + " create"}, Stderr: "database unreachable\n", Exit: 1})
	r = f.hl("backup")
	if r.code != 1 {
		t.Fatalf("want a failure: %s", r)
	}
	contains(t, "failed backup", r.errOut, "notes: backup failed", "database unreachable")
}

func TestBackupOnlyNonContractApps(t *testing.T) {
	f := newFake(t)
	f.lab("lab", "apps:\n  static: {path: '"+f.app("static", staticModel)+"'}\n")
	r := f.hl("backup")
	if r.code != 1 {
		t.Fatalf("want a failure: %s", r)
	}
	contains(t, "nothing to back up", r.errOut, "no app follows the backup contract")
}

func TestBackupNewerContract(t *testing.T) {
	f := newFake(t)
	f.lab("lab", "apps:\n  notes: {path: '"+f.app("notes", strings.Replace(notesModel, `"backup.contract":"1"`, `"backup.contract":"2"`, 1))+"'}\n")
	r := f.hl("backup")
	if r.code != 1 {
		t.Fatalf("want a failure: %s", r)
	}
	contains(t, "version", r.errOut, `contract version "2"`, "update hl")
	if runs := f.find("run --rm"); len(runs) > 0 {
		t.Errorf("ran a contract hl doesn't know: %v", runs)
	}
}

func TestBackupList(t *testing.T) {
	f, _ := backupLab(t)
	r := f.must("backup", "--list", "notes")
	contains(t, "empty list", r.out, "notes has no backups yet")

	f.on(rule{Match: []string{runBackupArgs + " list"},
		Stdout: newest + "\t1532000\t2026-11-23T03:00:00+00:00\t\n" + older + "\t812\t2026-11-22T03:00:00+00:00\tv2\n"})
	r = f.must("backup", "--list", "notes")
	contains(t, "list", r.out, "NAME", "SIZE", "CREATED", "TAG", newest, "1.5 MB", older, "812 B", "v2")
	if strings.Index(r.out, newest) > strings.Index(r.out, older) {
		t.Errorf("not in the backup's order:\n%s", r.out)
	}
}

func TestRestore(t *testing.T) {
	f, _ := backupLab(t)
	// latest skips a newer safety backup: going back to one is a choice made by name.
	f.on(rule{Match: []string{runBackupArgs + " list"}, Stdout: "notes-pre-restore-20261123-035000.tar.gz\t10\t2026-11-23T03:50:00+00:00\tpre-restore\n" +
		newest + "\t10\t2026-11-23T03:00:00+00:00\t\n" + older + "\t10\t2026-11-22T03:00:00+00:00\tv2\n"})
	f.on(rule{Match: []string{runBackupArgs + " restore"}, Stdout: "notes-pre-restore-20261123-040000.tar.gz\n"})

	r := f.must("restore", "notes", "latest", "--yes")
	contains(t, "restore", r.out, "verifying "+newest, "restored notes from "+newest, "notes-pre-restore-20261123-040000.tar.gz")
	var steps []string
	for _, c := range f.find("--project-name hl-notes") {
		s := c.String()
		for _, step := range []string{" list", " verify " + newest, "stop web", " restore " + newest, "start web"} {
			if strings.HasSuffix(s, step) {
				steps = append(steps, strings.TrimSpace(step))
			}
		}
	}
	want := []string{"list", "verify " + newest, "stop web", "restore " + newest, "start web"}
	if strings.Join(steps, "|") != strings.Join(want, "|") {
		t.Errorf("steps:\n got %q\nwant %q", steps, want)
	}
	if stops := f.find("stop"); len(stops) != 1 || strings.Contains(stops[0].String(), "db") {
		t.Errorf("only the backup.stop services stop, the database stays up: %v", stops)
	}

	// A named backup isn't looked up in the list.
	f.reset()
	f.must("restore", "notes", older, "--yes")
	if len(f.find(runBackupArgs+" list")) != 0 || len(f.find(runBackupArgs+" restore "+older)) != 1 {
		t.Errorf("restore by name: %v", f.calls())
	}
}

func TestRestoreAsks(t *testing.T) {
	f, _ := backupLab(t)

	// No terminal and no --yes: refused before anything runs.
	r := f.hl("restore", "notes", older)
	if r.code != 1 {
		t.Fatalf("want a refusal: %s", r)
	}
	contains(t, "no terminal", r.errOut, "not a terminal", "nothing changed", "--yes")
	if runs := f.find("run --rm"); len(runs) > 0 {
		t.Errorf("ran without asking: %v", runs)
	}

	// No (and Enter, the default) leaves it as it was, after the verify.
	for _, answer := range []string{"n\n", "\n"} {
		f.reset()
		r = f.hlInput(answer, "restore", "notes", older)
		if r.code != 0 {
			t.Fatalf("answer %q: %s", answer, r)
		}
		contains(t, "answer no", r.out, "Replace notes's data with "+older, "stops web", "nothing changed")
		if len(f.find(runBackupArgs+" verify "+older)) != 1 || len(f.find("stop")) > 0 || len(f.find(runBackupArgs+" restore")) > 0 {
			t.Errorf("answer %q: %v", answer, f.calls())
		}
	}

	// Yes restores.
	f.reset()
	if r = f.hlInput("y\n", "restore", "notes", older); r.code != 0 || len(f.find(runBackupArgs+" restore "+older)) != 1 {
		t.Errorf("answer yes: %s", r)
	}
}

func TestRestoreFailures(t *testing.T) {
	f, _ := backupLab(t)

	// A bad archive: nothing stops.
	f.on(rule{Match: []string{runBackupArgs + " verify bad.tar.gz"}, Stderr: "checksum mismatch\n", Exit: 3})
	r := f.hl("restore", "notes", "bad.tar.gz", "--yes")
	if r.code != 3 {
		t.Fatalf("want a failure: %s", r)
	}
	contains(t, "verify failed", r.errOut, "bad.tar.gz failed verification; nothing changed", "checksum mismatch")
	if len(f.find("stop")) > 0 {
		t.Errorf("stopped services for a bad archive: %v", f.calls())
	}

	// latest without backups, or with only safety backups.
	r = f.hl("restore", "notes", "latest", "--yes")
	if r.code != 1 || !strings.Contains(r.errOut, "notes has no backups") {
		t.Errorf("latest without backups: %s", r)
	}
	f.on(rule{Match: []string{runBackupArgs + " list"}, Stdout: "notes-pre-restore-20261123-035000.tar.gz\t10\tx\tpre-restore\n"})
	f.reset()
	r = f.hl("restore", "notes", "latest", "--yes")
	if r.code != 1 || !strings.Contains(r.errOut, "other than pre-restore") || len(f.find(runBackupArgs+" verify")) != 0 {
		t.Errorf("latest with only a safety backup: %s", r)
	}

	// A rolled-back restore still starts the services again.
	f.reset()
	f.on(rule{Match: []string{runBackupArgs + " restore " + older}, Stderr: "restore failed, rolling back\n", Exit: 4})
	r = f.hl("restore", "notes", older, "--yes")
	if r.code != 4 {
		t.Fatalf("want exit 4: %s", r)
	}
	contains(t, "rolled back", r.errOut, "rolled back; the old data is in place")
	if len(f.find("start web")) != 1 {
		t.Errorf("services not started again after a failed restore: %v", f.calls())
	}

	// The restore's own verify failing also passes 3 on.
	f.reset()
	f.on(rule{Match: []string{runBackupArgs + " restore third.tar.gz"}, Exit: 3})
	if r = f.hl("restore", "notes", "third.tar.gz", "--yes"); r.code != 3 || len(f.find("start web")) != 1 {
		t.Errorf("verify failed inside restore: %s", r)
	}

	// Any other failure: started again too, exit 1, and the safety backup is named.
	f.reset()
	f.on(rule{Match: []string{runBackupArgs + " restore other.tar.gz"}, Exit: 1})
	r = f.hl("restore", "notes", "other.tar.gz", "--yes")
	contains(t, "failed restore", r.errOut, "the restore failed", "pre-restore")
	if r.code != 1 || len(f.find("start web")) != 1 {
		t.Errorf("other failure: %s", r)
	}
}

func TestRestoreNeedsRunningApp(t *testing.T) {
	f, _ := backupLab(t, ctr("other-project", "web", "running", "Up", "")) // nothing of notes runs
	r := f.hl("restore", "notes", older, "--yes")
	if r.code != 1 {
		t.Fatalf("want a refusal: %s", r)
	}
	contains(t, "not running", r.errOut, "notes isn't running", "hl up notes")
	if runs := f.find("run --rm"); len(runs) > 0 {
		t.Errorf("ran against a stopped app: %v", runs)
	}
}

func TestRestoreWaitsForHealth(t *testing.T) {
	f, _ := backupLab(t, ctr("hl-notes", "web", "running", "Up 2 seconds (unhealthy)", ""), ctr("hl-notes", "db", "running", "Up 1 minute", ""))
	waitTimeout = 0
	r := f.hl("restore", "notes", older, "--yes")
	if r.code != 1 {
		t.Fatalf("want a failure: %s", r)
	}
	contains(t, "unhealthy", r.errOut, "notes: not running and healthy: web")
	if len(f.find("logs", "web")) != 1 {
		t.Errorf("no logs of the unhealthy service: %v", f.calls())
	}
}

func TestRestoreBadStopLabel(t *testing.T) {
	f := newFake(t)
	f.lab("lab", "apps:\n  notes: {path: '"+f.app("notes", strings.Replace(notesModel, `"backup.stop":"web"`, `"backup.stop":"web,worker"`, 1))+"'}\n")
	r := f.hl("restore", "notes", older, "--yes")
	if r.code != 1 {
		t.Fatalf("want a failure: %s", r)
	}
	contains(t, "bad label", r.errOut, `backup.stop label names "worker"`)

	f.lab("lab2", "apps:\n  notes: {path: '"+f.app("notes2", strings.Replace(notesModel, `,"backup.stop":"web"`, ``, 1))+"'}\n")
	r = f.hl("restore", "notes", older, "--yes")
	if r.code != 1 {
		t.Fatalf("want a failure: %s", r)
	}
	contains(t, "no label", r.errOut, "has no backup.stop label")
}
