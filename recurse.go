// Package recurse provides an external CoreDNS recursive resolver stub.
package recurse

import (
	"context"
	"errors"

	"github.com/coredns/coredns/plugin"
	"github.com/miekg/dns"
)

// ErrUnimplemented indicates that recursive resolution is not implemented.
var ErrUnimplemented = errors.New("recursive resolution is not implemented")

// Engine resolves a DNS request. Implementations must be safe for concurrent use,
// honor ctx, and return a complete response or an error. They must not write replies.
type Engine interface {
	Resolve(ctx context.Context, request *dns.Msg) (*dns.Msg, error)
}

// Recurse is a terminal handler; it never falls through to another plugin.
// A nil Engine behaves like the default, unimplemented engine.
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
		engine = unimplementedEngine{}
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

type unimplementedEngine struct{}

func (unimplementedEngine) Resolve(context.Context, *dns.Msg) (*dns.Msg, error) {
	return nil, ErrUnimplemented
}
