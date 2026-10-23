package config

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"gopkg.in/yaml.v3"
)

const schemaFile = "../../schema/hl.schema.json"

func compileSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	s, err := jsonschema.Compile(schemaFile)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// asJSON turns hl.yml content into the JSON value the schema validates.
func asJSON(t *testing.T, body string) any {
	t.Helper()
	var v any
	if err := yaml.Unmarshal([]byte(body), &v); err != nil {
		t.Fatalf("%v\n%s", err, body)
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var out any
	if err := d.Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

// readmeYAML is every ```yaml block in the README.
func readmeYAML(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	var blocks []string
	for _, m := range regexp.MustCompile("(?s)```yaml\n(.*?)```").FindAllStringSubmatch(string(b), -1) {
		blocks = append(blocks, m[1])
	}
	return blocks
}

func TestSchemaAcceptsREADMEAndTemplate(t *testing.T) {
	s := compileSchema(t)
	blocks := readmeYAML(t)
	if len(blocks) == 0 {
		t.Fatal("no yaml blocks in README.md")
	}
	for _, body := range append(blocks, Template, commented) {
		if err := s.Validate(asJSON(t, body)); err != nil {
			t.Errorf("schema rejects:\n%s\n%v", body, err)
		}
		// hl itself agrees.
		if ps := Check([]byte(body)); len(ps) > 0 {
			t.Errorf("Check rejects:\n%s\n%s", body, joinProblems(ps))
		}
	}
}

func TestSchemaRejectsMistakes(t *testing.T) {
	s := compileSchema(t)
	for _, body := range []string{
		"proxy: {bnd: 0.0.0.0}",
		"proxy: {routing: none}",
		"proxy: {port: 70000}",
		"version: 2",
		"apps: {My-App: {path: x}}",
		"apps: {my-app: {}}",
		"apps: {my-app: {path: x, routing: sideways}}",
		"apps: {my-app: {path: x, prefix: /my-app/}}",
		"apps: {my-app: {path: x, servce: web}}",
		"apps: {my-app: {path: x, compose: compose.yml}}",
		"projets: [x]",
		"proxy: {log_level: loud}",
		"apps: {proxy: {path: x}}",
		"scan_depth: -1",
		"projects: ~/code",
		"proxy: {project: My_Proxy}",
		"proxy: {access_log: yes please}",
	} {
		if err := s.Validate(asJSON(t, body)); err == nil {
			t.Errorf("schema accepts %q", body)
		}
		if ps := Check([]byte(body)); len(ps) == 0 {
			t.Errorf("Check accepts %q", body)
		}
	}
}

type schemaNode struct {
	ID          string                 `json:"$id"`
	Ref         string                 `json:"$ref"`
	Properties  map[string]*schemaNode `json:"properties"`
	Definitions map[string]*schemaNode `json:"definitions"`
	Additional  json.RawMessage        `json:"additionalProperties"`
	Enum        []any                  `json:"enum"`
	Default     any                    `json:"default"`
	Pattern     string                 `json:"pattern"`
	Names       *schemaNode            `json:"propertyNames"`
}

func loadSchemaNode(t *testing.T) *schemaNode {
	t.Helper()
	b, err := os.ReadFile(schemaFile)
	if err != nil {
		t.Fatal(err)
	}
	var n schemaNode
	if err := json.Unmarshal(b, &n); err != nil {
		t.Fatal(err)
	}
	return &n
}

func keys[T any](m map[string]T) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// The schema describes exactly the keys the Go types read, so neither can drift: a field added
// to Config, Proxy or App must be added to schema/hl.schema.json too.
func TestSchemaMatchesTypes(t *testing.T) {
	root := loadSchemaNode(t)
	app := root.Definitions["app"]
	for _, tc := range []struct {
		name   string
		schema *schemaNode
		typ    any
	}{
		{"top level", root, Config{}},
		{"proxy", root.Properties["proxy"], Proxy{}},
		{"apps.<app>", app, App{}},
	} {
		got, want := keys(tc.schema.Properties), keys(yamlFields(reflect.TypeOf(tc.typ)))
		if strings.Join(got, " ") != strings.Join(want, " ") {
			t.Errorf("%s: schema has %v, Go type has %v", tc.name, got, want)
		}
	}
	if root.ID != SchemaURL {
		t.Errorf("$id = %s, want SchemaURL %s", root.ID, SchemaURL)
	}
	if v := root.Properties["version"].Enum; len(v) != 1 || v[0] != float64(Version) {
		t.Errorf("version enum = %v, want [%d]", v, Version)
	}
	if p := root.Properties["apps"].Names.Pattern; p != nameRE.String() {
		t.Errorf("app name pattern %s, Go uses %s", p, nameRE)
	}
	if p := app.Properties["prefix"].Pattern; p != prefixRE.String() {
		t.Errorf("prefix pattern %s, Go uses %s", p, prefixRE)
	}
	if got := jsonString(root.Properties["proxy"].Properties["routing"].Enum); got != jsonString([]string{RoutingPath, RoutingHost}) {
		t.Errorf("proxy.routing enum %s", got)
	}
	var levels []string
	for _, l := range ProxyLogLevels {
		levels = append(levels, l)
	}
	for _, l := range ProxyLogLevels {
		levels = append(levels, strings.ToLower(l))
	}
	if got := jsonString(root.Properties["proxy"].Properties["log_level"].Enum); got != jsonString(levels) {
		t.Errorf("proxy.log_level enum %s, want %s", got, jsonString(levels))
	}
	if p := root.Properties["proxy"].Properties["project"].Pattern; p != projectRE.String() {
		t.Errorf("proxy.project pattern %s, Go uses %s", p, projectRE)
	}
	if got := jsonString(app.Properties["routing"].Enum); got != jsonString([]string{RoutingPath, RoutingHost, RoutingNone}) {
		t.Errorf("app routing enum %s", got)
	}
}

func jsonString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// Defaults shown to editors are hl's real defaults.
func TestSchemaDefaultsAreHlDefaults(t *testing.T) {
	var c Config
	c.defaults()
	pv := reflect.ValueOf(c.Proxy)
	pt := pv.Type()
	props := loadSchemaNode(t).Properties["proxy"].Properties
	for i := 0; i < pt.NumField(); i++ {
		name, _, _ := strings.Cut(pt.Field(i).Tag.Get("yaml"), ",")
		def := pv.Field(i).Interface()
		got := props[name].Default
		if reflect.ValueOf(def).IsZero() {
			if got != nil && jsonString(got) != jsonString(def) {
				t.Errorf("proxy.%s: schema default %v, hl has none", name, got)
			}
			continue
		}
		if jsonString(got) != jsonString(def) {
			t.Errorf("proxy.%s: schema default %v, hl's is %v", name, got, def)
		}
	}
}
