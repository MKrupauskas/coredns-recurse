package recurse

import (
	"github.com/coredns/caddy"
	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"
)

func init() { plugin.Register("recurse", setup) }

func setup(c *caddy.Controller) error {
	seen := false
	for c.Next() {
		if seen {
			return plugin.Error("recurse", plugin.ErrOnce)
		}
		seen = true
		if c.NextArg() {
			return plugin.Error("recurse", c.ArgErr())
		}
		if c.NextBlock() {
			return plugin.Error("recurse", c.Err("recurse does not accept a block"))
		}
	}
	dnsserver.GetConfig(c).AddPlugin(func(_ plugin.Handler) plugin.Handler {
		return Recurse{Engine: unimplementedEngine{}}
	})
	return nil
}
