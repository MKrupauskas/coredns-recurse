// Package recurse embeds the SDNS recursive resolver in an external CoreDNS plugin.
package recurse

import (
	"context"
	"errors"

	"github.com/coredns/coredns/plugin"
	"github.com/miekg/dns"
)

// ErrNoEngine indicates a handler that was not initialized by CoreDNS setup.
var ErrNoEngine = errors.New("recursive resolver is not initialized")

// Engine resolves a DNS request. Implementations must be safe for concurrent use,
// honor ctx, and return a complete response or an error. They must not write replies.
type Engine interface {
	Resolve(ctx context.Context, request *dns.Msg) (*dns.Msg, error)
}

// Recurse is a terminal handler; it never falls through to another plugin.
// A nil Engine fails closed; setup installs the SDNS engine.
type Recurse struct {
	Engine Engine
}

var _ plugin.Handler = Recurse{}

// Name implements plugin.Handler.
func (Recurse) Name() string { return "recurse" }

// ServeDNS implements plugin.Handler. On failure, CoreDNS writes the SERVFAIL
// response; returning it without writing avoids duplicate replies.
func (p Recurse) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	engine := p.Engine
	if engine == nil {
		return dns.RcodeServerFailure, plugin.Error(p.Name(), ErrNoEngine)
	}
	response, err := engine.Resolve(ctx, r)
	if err != nil {
		return dns.RcodeServerFailure, plugin.Error(p.Name(), err)
	}
	if response == nil {
		return dns.RcodeServerFailure, plugin.Error(p.Name(), errors.New("engine returned no response"))
	}
	// Preserve CoreDNS's convention that these codes mean no reply was written.
	if !plugin.ClientWrite(response.Rcode) {
		return response.Rcode, nil
	}
	if err := w.WriteMsg(response); err != nil {
		return dns.RcodeServerFailure, plugin.Error(p.Name(), err)
	}
	return response.Rcode, nil
}
