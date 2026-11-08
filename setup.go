package recurse

import (
	"net"
	"os"
	"strconv"
	"time"

	"github.com/coredns/caddy"
	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"
	"github.com/semihalev/sdns/config"
)

func init() { plugin.Register("recurse", setup) }

func setup(c *caddy.Controller) error {
	seen := false
	roots := rootHints
	timeout := 10 * time.Second
	var sdnsConfig *config.Config
	for c.Next() {
		if seen {
			return plugin.Error("recurse", plugin.ErrOnce)
		}
		seen = true
		args := c.RemainingArgs()
		if len(args) > 1 {
			return plugin.Error("recurse", c.ArgErr())
		}
		if len(args) == 1 {
			// Load performs SDNS's ordinary validation/defaulting and initializes
			// its state directory and cookie secret. Require an existing file so
			// a misspelled path cannot silently generate a different config.
			if _, err := os.Stat(args[0]); err != nil {
				return plugin.Error("recurse", err)
			}
			var err error
			sdnsConfig, err = config.Load(args[0], "coredns-recurse")
			if err != nil {
				return plugin.Error("recurse", err)
			}
		}
		seenRoots, seenTimeout := false, false
		for c.NextBlock() {
			if sdnsConfig != nil {
				return plugin.Error("recurse", c.Err("use SDNS configuration or block options, not both"))
			}
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
	var engine *sdnsEngine
	if sdnsConfig != nil {
		engine = engineFromConfig(sdnsConfig)
	} else {
		engine = newSDNSEngine(roots, timeout)
	}
	c.OnStartup(engine.start)
	c.OnShutdown(engine.close)
	dnsserver.GetConfig(c).AddPlugin(func(_ plugin.Handler) plugin.Handler {
		return Recurse{Engine: engine}
	})
	return nil
}
