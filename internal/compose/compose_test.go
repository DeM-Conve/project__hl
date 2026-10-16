package compose

import "testing"

func TestExposed(t *testing.T) {
	one := &Model{Services: map[string]Service{"web": {Ports: []Port{{Target: 3000}}}, "db": {}}}
	if s, err := one.Exposed(); err != nil || s != "web" {
		t.Errorf("one published: %q %v", s, err)
	}
	if p, _ := one.TargetPort("web"); p != 3000 {
		t.Errorf("TargetPort = %d", p)
	}
	several := &Model{Services: map[string]Service{"api": {Ports: []Port{{Target: 8080}}}, "frontend": {Ports: []Port{{Target: 80}}}}}
	if s, err := several.Exposed(); err != nil || s != "frontend" {
		t.Errorf("several, one conventional: %q %v", s, err)
	}
	ambiguous := &Model{Services: map[string]Service{"a": {Ports: []Port{{Target: 1}}}, "b": {Ports: []Port{{Target: 2}}}}}
	if _, err := ambiguous.Exposed(); err == nil {
		t.Error("ambiguous: expected an error")
	}
	none := &Model{Services: map[string]Service{"db": {}}}
	if _, err := none.Exposed(); err == nil {
		t.Error("none published: expected an error")
	}
}
