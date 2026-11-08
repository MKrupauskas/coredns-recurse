package recurse

import (
	"context"
	"errors"
	"testing"

	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/pkg/dnstest"
	"github.com/coredns/coredns/plugin/test"
	"github.com/miekg/dns"
)

type engineFunc func(context.Context, *dns.Msg) (*dns.Msg, error)

func (f engineFunc) Resolve(ctx context.Context, r *dns.Msg) (*dns.Msg, error) { return f(ctx, r) }

func TestFailuresNeverWriteAnswer(t *testing.T) {
	for _, tc := range []struct {
		name   string
		engine Engine
		want   error
	}{
		{"zero value", nil, ErrNoEngine},
		{"nil response", engineFunc(func(context.Context, *dns.Msg) (*dns.Msg, error) { return nil, nil }), nil},
		{"response with error", engineFunc(func(context.Context, *dns.Msg) (*dns.Msg, error) { return new(dns.Msg), ErrNoEngine }), ErrNoEngine},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := dnstest.NewRecorder(&test.ResponseWriter{})
			r := new(dns.Msg).SetQuestion("example.org.", dns.TypeA)
			code, err := (Recurse{Engine: tc.engine}).ServeDNS(context.Background(), w, r)
			if code != dns.RcodeServerFailure || err == nil {
				t.Fatalf("got (%d, %v)", code, err)
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("error = %v", err)
			}
			if w.Msg != nil {
				t.Fatal("failure wrote a response")
			}
		})
	}
}

func TestInjectedEngine(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := new(dns.Msg).SetQuestion("example.org.", dns.TypeA)
	for _, code := range []int{dns.RcodeSuccess, dns.RcodeNameError, dns.RcodeServerFailure, dns.RcodeRefused} {
		w := dnstest.NewRecorder(&test.ResponseWriter{})
		called := false
		engine := engineFunc(func(got context.Context, query *dns.Msg) (*dns.Msg, error) {
			called = true
			if got != ctx || query != r {
				t.Fatal("context or request not passed through")
			}
			return new(dns.Msg).SetRcode(query, code), nil
		})
		got, err := (Recurse{Engine: engine}).ServeDNS(ctx, w, r)
		if !called || got != code || err != nil {
			t.Fatalf("got (%d, %v), called %v", got, err, called)
		}
		if (w.Msg != nil) != plugin.ClientWrite(code) {
			t.Fatalf("unexpected write for rcode %d", code)
		}
	}
}
