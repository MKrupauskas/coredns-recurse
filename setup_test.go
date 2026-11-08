package recurse

import (
	"testing"

	"github.com/coredns/caddy"
	"github.com/coredns/coredns/core/dnsserver"
)

func TestSetup(t *testing.T) {
	for _, tc := range []struct {
		input string
		valid bool
	}{
		{"recurse", true},
		{"recurse {\n root 127.0.0.1:1054\n timeout 1s\n}", true},
		{"recurse {\n root 198.41.0.4:53 192.33.4.12:53\n}", true},
		{"recurse extra", false},
		{"recurse {\n option\n}", false},
		{"recurse {\n root\n}", false},
		{"recurse {\n root localhost:53\n}", false},
		{"recurse {\n root 127.0.0.1:0\n}", false},
		{"recurse {\n root 127.0.0.1:65536\n}", false},
		{"recurse {\n root [::1]:53\n}", false},
		{"recurse {\n root 127.0.0.1:53\n root 127.0.0.2:53\n}", false},
		{"recurse {\n timeout\n}", false},
		{"recurse {\n timeout 0s\n}", false},
		{"recurse {\n timeout -1s\n}", false},
		{"recurse {\n timeout bad\n}", false},
		{"recurse {\n timeout 1s extra\n}", false},
		{"recurse {\n timeout 1s\n timeout 2s\n}", false},
		{"recurse\nrecurse", false},
	} {
		t.Run(tc.input, func(t *testing.T) {
			c := caddy.NewTestController("dns", tc.input)
			err := setup(c)
			if (err == nil) != tc.valid {
				t.Fatalf("setup error = %v; valid = %v", err, tc.valid)
			}
			if tc.valid {
				plugins := dnsserver.GetConfig(c).Plugin
				if len(plugins) != 1 {
					t.Fatalf("registered %d plugins", len(plugins))
				}
				handler := plugins[0](nil)
				if handler.Name() != "recurse" {
					t.Fatalf("name = %q", handler.Name())
				}
				engine, ok := handler.(Recurse).Engine.(*sdnsEngine)
				if !ok {
					t.Fatal("default engine is not SDNS")
				}
				t.Cleanup(func() { _ = engine.close() })
			}
		})
	}
}
