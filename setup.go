package recurse

import (
	"os"

	"github.com/coredns/caddy"
	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"
	"github.com/semihalev/sdns/config"
)

func init() { plugin.Register("recurse", setup) }

func setup(c *caddy.Controller) error {
	seen := false
	var sdnsConfig *config.Config
	for c.Next() {
		if seen {
			return plugin.Error("recurse", plugin.ErrOnce)
		}
		seen = true
		args := c.RemainingArgs()
		if len(args) != 1 {
			return plugin.Error("recurse", c.Err("expected an SDNS configuration file: recurse CONFIGFILE"))
		}
		if c.NextBlock() {
			return plugin.Error("recurse", c.Err("configure resolver options in the SDNS file"))
		}
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
	engine := engineFromConfig(sdnsConfig)
	c.OnStartup(engine.start)
	c.OnShutdown(engine.close)
	dnsserver.GetConfig(c).AddPlugin(func(_ plugin.Handler) plugin.Handler {
		return Recurse{Engine: engine}
	})
	return nil
}
