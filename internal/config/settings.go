package config

import (
	"fmt"
	"github.com/DeM-Conve/project__hl/internal/defaults"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"gopkg.in/yaml.v3"
)

// Setting is one hl.yml setting that has a default: the value in effect and where it came
// from ("hl.yml" or "default").
type Setting struct {
	Key, Default, Value, Source string
}

// Settings lists the homelab-wide settings of <dir>/hl.yml (everything but apps and env), each
// with hl's default and the value in effect. The defaults are read by applying hl's own
// defaulting to an empty config, so the list follows wherever those defaults are defined.
func Settings(dir string) ([]Setting, error) {
	b, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		return nil, err
	}
	var raw Config
	if err := yaml.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("%s: %w - run hl config check", filepath.Join(dir, FileName), err)
	}
	var def Config
	def.defaults()
	eff := raw
	eff.defaults()
	var out []Setting
	listSettings(reflect.ValueOf(def), reflect.ValueOf(raw), reflect.ValueOf(eff), "", &out)
	return out, nil
}

func listSettings(def, raw, eff reflect.Value, prefix string, out *[]Setting) {
	t := def.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("yaml"), ",")
		if !f.IsExported() || name == "-" || name == "version" {
			continue
		}
		key := join(prefix, name)
		switch f.Type.Kind() {
		case reflect.Struct:
			listSettings(def.Field(i), raw.Field(i), eff.Field(i), key, out)
			continue
		case reflect.Map:
			continue // apps and env: per-name settings, no defaults
		}
		s := Setting{Key: key, Default: show(def.Field(i)), Value: show(eff.Field(i)), Source: "default"}
		// Left empty in hl.yml's defaults but filled in where it's used.
		if d, ok := appliedLater[key]; ok {
			if s.Default == "-" {
				s.Default = d
			}
			if s.Value == "-" {
				s.Value = d
			}
		}
		if !raw.Field(i).IsZero() {
			s.Source = FileName
		}
		*out = append(*out, s)
	}
}

// appliedLater are settings defaults() leaves empty because the empty value
// means something (e.g. log_level can't be combined with proxy.config), but
// whose effective default is applied where the setting is used.
var appliedLater = map[string]string{
	"proxy.log_level": defaults.DefaultProxyLogLevel,
}

func show(v reflect.Value) string {
	if v.Kind() == reflect.Bool {
		return fmt.Sprint(v.Bool())
	}
	if v.IsZero() {
		return "-"
	}
	if v.Kind() == reflect.Slice {
		parts := make([]string, v.Len())
		for i := range parts {
			parts[i] = fmt.Sprint(v.Index(i).Interface())
		}
		return strings.Join(parts, ", ")
	}
	return fmt.Sprint(v.Interface())
}
