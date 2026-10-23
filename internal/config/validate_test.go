package config

import (
	"strings"
	"testing"
)

func TestCheckFindsProblemsWithLines(t *testing.T) {
	body := `version: 1
proxy:
  bnd: 0.0.0.0
  port: eighty
  routing: hots
apps:
  my-app:
    path: ~/code/my-app
    servce: web
    port: 70000
    prefix: /my-app/
  My App:
    path: x
  no-path:
    service: web
  blog:
    path: ~/code/blog
    compose: compose.yml
    routing: none
    routing: host
`
	ps := Check([]byte(body))
	want := []struct {
		line int
		key  string
		has  string
	}{
		{3, "proxy.bnd", `did you mean "bind"?`},
		{4, "proxy.port", `"eighty" is not a whole number`},
		{5, "proxy.routing", `did you mean "host"?`},
		{9, "apps.my-app.servce", `did you mean "service"?`},
		{10, "apps.my-app.port", "from 1 to 65535"},
		{11, "apps.my-app.prefix", "without a trailing slash"},
		{12, "apps.My App", "use lowercase"},
		{15, "apps.no-path", "path is required"},
		{18, "apps.blog.compose", "expected a list"},
		{20, "apps.blog.routing", "set twice (also on line 19)"},
	}
	if len(ps) != len(want) {
		t.Fatalf("got %d problems, want %d:\n%s", len(ps), len(want), joinProblems(ps))
	}
	for i, w := range want {
		p := ps[i]
		if p.Line != w.line || p.Key != w.key || !strings.Contains(p.Message, w.has) {
			t.Errorf("problem %d = %s; want line %d, key %s, containing %q", i, p, w.line, w.key, w.has)
		}
	}
}

func joinProblems(ps []Problem) string {
	var s []string
	for _, p := range ps {
		s = append(s, p.In(FileName))
	}
	return strings.Join(s, "\n")
}

func TestCheckSyntaxAndShape(t *testing.T) {
	for body, has := range map[string]string{
		"proxy:\n  bind: [\n":         "not valid YAML",
		"- a\n- b\n":                  "at the top",
		"apps:\n  my-app: [x]\n":      "expected settings",
		"version: 2\n":                "update hl",
		"env: [TZ=UTC]\n":             "expected a list of key: value",
		"proxy:\n  port: [80]\n":      "single value",
		"apps:\n  my-app: {}\n":       "path is required",
		"apps:\n  my-app:\n":          "path is required",
		"apps:\n  my-app: {pth: x}\n": `did you mean "path"?`,
		"proxy: {log_level: debg}":    `did you mean "DEBUG"?`,
		"proxy: {project: My_Proxy}":  "lowercase",
		"proxy: {access_log: on}":     "not true or false",
	} {
		ps := Check([]byte(body))
		if len(ps) == 0 || !strings.Contains(joinProblems(ps), has) {
			t.Errorf("%q: want a problem containing %q, got:\n%s", body, has, joinProblems(ps))
		}
	}
}

func TestCheckAcceptsValidFiles(t *testing.T) {
	for _, body := range []string{
		"",
		"apps: {}\n",
		Template,
		`
version: 1
proxy: {bind: 0.0.0.0, port: 8080, routing: host, domain: example.com}
env: {TZ: UTC, WORKERS: 4, DEBUG: false}
apps:
  my-app.io:
    path: ~/code/my-app
    compose: [compose.yml, compose.prod.yml]
    env_files: [my-app.env]
    env: {BASE: "${HL_PREFIX}"}
  db: {path: db, routing: none}
`,
	} {
		if ps := Check([]byte(body)); len(ps) > 0 {
			t.Errorf("%q:\n%s", body, joinProblems(ps))
		}
	}
}

func TestUnknownKeyWithoutCloseMatchListsKnownKeys(t *testing.T) {
	ps := Check([]byte("proxy:\n  zzzzzz: 1\n"))
	if len(ps) != 1 || !strings.Contains(ps[0].Message, "known keys here: ") || !strings.Contains(ps[0].Message, " bind,") {
		t.Errorf("%v", ps)
	}
}

func TestCheckFile(t *testing.T) {
	dir := write(t, "proxy:\n  prot: 80\n")
	err := CheckFile(dir)
	if err == nil || !strings.Contains(err.Error(), "hl.yml:2:3: proxy.prot: unknown key") {
		t.Errorf("CheckFile = %v", err)
	}
	if err := CheckFile(write(t, Template)); err != nil {
		t.Error(err)
	}
}
