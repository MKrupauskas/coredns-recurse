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
		{"recurse extra", false},
		{"recurse {\n option\n}", false},
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
				if _, ok := handler.(Recurse).Engine.(unimplementedEngine); !ok {
					t.Fatal("default engine is not the stub")
				}
			}
		})
	}
}
