package recurse

import (
	"net"
	"strconv"
	"time"

	"github.com/coredns/caddy"
	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"
)

func init() { plugin.Register("recurse", setup) }

func setup(c *caddy.Controller) error {
	seen := false
	roots := rootHints
	timeout := 10 * time.Second
	for c.Next() {
		if seen {
			return plugin.Error("recurse", plugin.ErrOnce)
		}
		seen = true
		if len(c.RemainingArgs()) != 0 {
			return plugin.Error("recurse", c.ArgErr())
		}
		seenRoots, seenTimeout := false, false
		for c.NextBlock() {
			option := c.Val()
			args := c.RemainingArgs()
			switch option {
			case "root":
				if seenRoots || len(args) == 0 {
					return plugin.Error("recurse", c.ArgErr())
				}
				seenRoots = true
				for _, addr := range args {
					host, port, err := net.SplitHostPort(addr)
					ip := net.ParseIP(host)
					n, portErr := strconv.Atoi(port)
					if err != nil || ip == nil || ip.To4() == nil || portErr != nil || n < 1 || n > 65535 {
						return plugin.Error("recurse", c.Errf("root must be an IPv4 address:port: %s", addr))
					}
				}
				roots = args
			case "timeout":
				if seenTimeout || len(args) != 1 {
					return plugin.Error("recurse", c.ArgErr())
				}
				seenTimeout = true
				var err error
				timeout, err = time.ParseDuration(args[0])
				if err != nil || timeout <= 0 {
					return plugin.Error("recurse", c.Err("timeout must be a positive duration"))
				}
			default:
				return plugin.Error("recurse", c.Errf("unknown option: %s", option))
			}
		}
	}
	engine := newSDNSEngine(roots, timeout)
	c.OnShutdown(engine.close)
	c.OnRestart(engine.close)
	dnsserver.GetConfig(c).AddPlugin(func(_ plugin.Handler) plugin.Handler {
		return Recurse{Engine: engine}
	})
	return nil
}
