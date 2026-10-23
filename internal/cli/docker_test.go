package cli

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/DeM-Conve/project__hl/internal/config"
)

func TestDockerHint(t *testing.T) {
	for msg, want := range map[string]string{
		"Got permission denied while trying to connect to the Docker daemon socket": "docker group",
		"Cannot connect to the Docker daemon at unix:///var/run/docker.sock":        "start Docker",
		"Bind for 0.0.0.0:80 failed: port is already allocated":                     "proxy.port",
		"yaml: unknown tag !reset":                                               "v2.24",
		"Pool overlaps with other one on this address space":                     "proxy.subnet",
		"could not find an available, non-overlapping IPv4 address pool":         "docker network prune",
		"network hl-proxy declared as external, but could not be found":          "hl proxy up",
		`Conflict. The container name "/x" is already in use by container "abc"`: "docker rm -f",
		"pull access denied for nope, repository does not exist":                 "docker login",
		"write /var/lib/docker/x: no space left on device":                       "disk is full",
		"something else entirely":                                                "",
	} {
		if got := dockerHint(msg); (want == "") != (got == "") || !strings.Contains(got, want) {
			t.Errorf("%q: hint %q, want it to mention %q", msg, got, want)
		}
	}
}

func TestPortChecks(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip(err)
	}
	defer func() { _ = l.Close() }()
	port := l.Addr().(*net.TCPAddr).Port
	if !addrInUse("127.0.0.1", port) {
		t.Error("a listening port is in use")
	}
	l2, _ := net.Listen("tcp", "127.0.0.1:0")
	free := l2.Addr().(*net.TCPAddr).Port
	_ = l2.Close()
	if addrInUse("127.0.0.1", free) {
		t.Error("a closed port is free")
	}

	// The program is named by lsof when docker doesn't know it.
	f := newFake(t)
	if err := os.Symlink(filepath.Join(f.bin, "docker"), filepath.Join(f.bin, "lsof")); err != nil {
		t.Fatal(err)
	}
	f.on(rule{Name: "lsof", Match: []string{"-iTCP:8080"}, Stdout: "p4242\ncnginx\n"})
	if got := whoUses(context.Background(), 8080); got != "nginx (pid 4242)" {
		t.Errorf("whoUses = %q", got)
	}
	if got := whoUses(context.Background(), 9999); got != "" {
		t.Errorf("unknown = %q", got)
	}
}

func TestSubnets(t *testing.T) {
	cidr := func(s string) *net.IPNet { _, n, _ := net.ParseCIDR(s); return n }
	if !overlaps(cidr("172.30.0.0/16"), cidr("172.30.5.0/24")) || overlaps(cidr("172.30.0.0/16"), cidr("172.31.0.0/16")) {
		t.Error("overlaps")
	}
	used := []*net.IPNet{cidr("172.31.0.0/16"), cidr("172.30.1.0/24")}
	if got := freeSubnet(used); got != "172.29.0.0/16" {
		t.Errorf("freeSubnet = %q", got)
	}
	var all []*net.IPNet
	for i := 16; i <= 31; i++ {
		all = append(all, cidr("172."+strconv.Itoa(i)+".0.0/16"))
	}
	if freeSubnet(all) != "" {
		t.Error("nothing is free")
	}

	f := newFake(t)
	cfg := &config.Config{Dir: f.home, Proxy: config.Proxy{Network: "hl-proxy", Subnet: "172.30.0.0/16"}}
	f.on(rule{Match: []string{"network ls"}, Stdout: "id1\n"})
	f.on(rule{Match: []string{"network inspect", "id1"}, Stdout: "hl-proxy\t172.29.0.0/16 \n"})
	err := checkNetwork(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), "exists with subnet 172.29.0.0/16") || !strings.Contains(hintFor(err), "hl down") {
		t.Errorf("existing network with another subnet: %v / %s", err, hintFor(err))
	}
	cfg.Proxy.Network, cfg.Proxy.Subnet = "another", "192.168.0.0/16"
	hostSubnets = func() []*net.IPNet { return []*net.IPNet{cidr("192.168.1.0/24")} }
	err = checkNetwork(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), "this machine's network 192.168.1.0/24") {
		t.Errorf("host network clash: %v", err)
	}
	cfg.Proxy.Subnet = "10.99.0.0/16"
	if err := checkNetwork(context.Background(), cfg); err != nil {
		t.Errorf("free subnet: %v", err)
	}
	cfg.Proxy.Subnet = ""
	if err := checkNetwork(context.Background(), cfg); err != nil {
		t.Errorf("no subnet: %v", err)
	}
}
