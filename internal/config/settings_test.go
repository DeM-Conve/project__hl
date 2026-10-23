package config

import "testing"

func TestSettings(t *testing.T) {
	dir := write(t, "version: 1\nproxy:\n  bind: 0.0.0.0\n  port: 8080\n")
	ss, err := Settings(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Setting{}
	for _, s := range ss {
		got[s.Key] = s
	}
	var def Config
	def.defaults()
	want := map[string]Setting{
		"proxy.bind":    {"proxy.bind", def.Proxy.Bind, "0.0.0.0", FileName},
		"proxy.port":    {"proxy.port", "80", "8080", FileName},
		"proxy.routing": {"proxy.routing", RoutingPath, RoutingPath, "default"},
		"proxy.subnet":  {"proxy.subnet", "-", "-", "default"},
		"proxy.image":   {"proxy.image", def.Proxy.Image, def.Proxy.Image, "default"},
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s = %+v, want %+v", k, got[k], w)
		}
	}
	for _, k := range []string{"apps", "env", "version"} {
		if _, ok := got[k]; ok {
			t.Errorf("%s should not be listed", k)
		}
	}
}
