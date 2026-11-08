package recurse

import (
	"context"
	"errors"
	"net"
	"testing"

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

func TestReplyTruncationAndFailureDetails(t *testing.T) {
	r := new(dns.Msg).SetQuestion("large.example.", dns.TypeA)
	e := engineFunc(func(context.Context, *dns.Msg) (*dns.Msg, error) {
		m := new(dns.Msg).SetReply(r)
		for i := range 60 {
			m.Answer = append(m.Answer, &dns.A{Hdr: dns.RR_Header{Name: r.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60}, A: net.IPv4(192, 0, 2, byte(i))})
		}
		return m, nil
	})
	w := dnstest.NewRecorder(&test.ResponseWriter{})
	if _, err := (Recurse{Engine: e}).ServeDNS(context.Background(), w, r); err != nil {
		t.Fatal(err)
	}
	if !w.Msg.Truncated || w.Msg.Len() > dns.MinMsgSize {
		t.Fatalf("oversized UDP reply: %v", w.Msg)
	}

	r.SetEdns0(1232, true)
	e = engineFunc(func(context.Context, *dns.Msg) (*dns.Msg, error) {
		m := new(dns.Msg).SetRcode(r, dns.RcodeServerFailure)
		m.SetEdns0(1232, true)
		m.IsEdns0().Option = append(m.IsEdns0().Option, &dns.EDNS0_EDE{InfoCode: dns.ExtendedErrorCodeDNSBogus, ExtraText: "DNSSEC validation failed"})
		return m, nil
	})
	w = dnstest.NewRecorder(&test.ResponseWriter{})
	if code, err := (Recurse{Engine: e}).ServeDNS(context.Background(), w, r); code != dns.RcodeSuccess || err != nil {
		t.Fatalf("%d %v", code, err)
	}
	if w.Msg == nil || w.Msg.Rcode != dns.RcodeServerFailure || len(w.Msg.IsEdns0().Option) != 1 {
		t.Fatal("SDNS failure details were lost")
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
		if !called || got != dns.RcodeSuccess || err != nil {
			t.Fatalf("got (%d, %v), called %v", got, err, called)
		}
		if w.Msg == nil || w.Msg.Rcode != code {
			t.Fatalf("unexpected write for rcode %d", code)
		}
	}
}
