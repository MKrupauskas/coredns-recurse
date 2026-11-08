package recurse

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/coredns/caddy"
	"github.com/coredns/coredns/core/dnsserver"
)

func TestSetup(t *testing.T) {
	path := testSDNSConfig(t, "192.0.2.1:53")
	noRoots := filepath.Join(t.TempDir(), "sdns.conf")
	if err := os.WriteFile(noRoots, []byte(fmt.Sprintf("directory = %q\ndnssec = \"off\"\n", t.TempDir())), 0600); err != nil {
		t.Fatal(err)
	}
	for name, input := range map[string]string{
		"missing config argument": "recurse",
		"extra argument":          "recurse " + path + " extra",
		"missing file":            "recurse " + path + ".missing",
		"missing root hints":      "recurse " + noRoots,
		"legacy block":            "recurse {\n root 127.0.0.1:53\n}",
		"config and block":        "recurse " + path + " {\n timeout 1s\n}",
		"duplicate directive":     "recurse " + path + "\nrecurse " + path,
	} {
		t.Run(name, func(t *testing.T) {
			if err := setup(caddy.NewTestController("dns", input)); err == nil {
				t.Fatal("invalid configuration was accepted")
			}
		})
	}
}

func TestSetupSDNSConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sdns.conf")
	data := fmt.Sprintf("directory = %q\ndnssec = \"off\"\nrootservers = [\"192.0.2.1:53\"]\n", dir)
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	c := caddy.NewTestController("dns", "recurse "+path)
	if err := setup(c); err != nil {
		t.Fatal(err)
	}
	plugins := dnsserver.GetConfig(c).Plugin
	if len(plugins) != 1 {
		t.Fatalf("registered %d plugins", len(plugins))
	}
	handler := plugins[0](nil)
	if handler.Name() != "recurse" {
		t.Fatalf("name = %q", handler.Name())
	}
	e := handler.(Recurse).Engine.(*sdnsEngine)
	defer e.close()
	if e.cfg.Directory != dir || e.cfg.RootServers[0] != "192.0.2.1:53" || e.pipeline != nil {
		t.Fatal("SDNS config was not loaded without starting workers during parsing")
	}
}
