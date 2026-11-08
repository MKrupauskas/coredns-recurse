package recurse

import (
	"context"
	"errors"
	"net"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coredns/coredns/plugin/pkg/dnstest"
	"github.com/coredns/coredns/plugin/test"
	"github.com/miekg/dns"
	"github.com/semihalev/sdns/middleware"
)

// A real UDP authority on an unprivileged port. Referral walking is tested in
// SDNS's embedded_test.go; the binary smoke test also performs public recursion.
func testAuthority(t *testing.T) (string, *atomic.Int32) {
	t.Helper()
	conn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	calls := new(atomic.Int32)
	server := &dns.Server{PacketConn: conn, Handler: dns.HandlerFunc(func(w dns.ResponseWriter, r *dns.Msg) {
		calls.Add(1)
		m := new(dns.Msg).SetReply(r)
		m.Authoritative = true
		q := r.Question[0]
		if q.Name == "alias." && q.Qtype == dns.TypeA {
			m.Answer = []dns.RR{&dns.CNAME{Hdr: dns.RR_Header{Name: q.Name, Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: 60}, Target: "example."}}
		} else if q.Name == "missing." {
			m.Rcode = dns.RcodeNameError
			soa, _ := dns.NewRR(". 60 IN SOA ns. hostmaster. 1 3600 600 86400 60")
			m.Ns = []dns.RR{soa}
		} else if q.Qtype == dns.TypeA {
			m.Answer = []dns.RR{&dns.A{Hdr: dns.RR_Header{Name: q.Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60}, A: net.IPv4(192, 0, 2, 42)}}
		}
		_ = w.WriteMsg(m)
	})}
	ready := make(chan struct{})
	server.NotifyStartedFunc = func() { close(ready) }
	go func() { _ = server.ActivateAndServe() }()
	<-ready
	t.Cleanup(func() { _ = server.Shutdown() })
	return conn.LocalAddr().String(), calls
}

func TestSDNSEngine(t *testing.T) {
	addr, calls := testAuthority(t)
	wasReady := middleware.Ready()
	engine := newSDNSEngine([]string{addr}, time.Second)
	t.Cleanup(func() { _ = engine.close() })
	for _, name := range []string{"example.", "missing.", "alias."} {
		r := new(dns.Msg).SetQuestion(name, dns.TypeA)
		r.SetEdns0(1232, true)
		original := r.Copy()
		w := dnstest.NewRecorder(&test.ResponseWriter{})
		code, err := (Recurse{Engine: engine}).ServeDNS(context.Background(), w, r)
		want := dns.RcodeSuccess
		if name == "missing." {
			want = dns.RcodeNameError
		}
		if err != nil || code != dns.RcodeSuccess || w.Msg == nil || w.Msg.Rcode != want {
			t.Fatalf("%s: code=%d err=%v response=%v", name, code, err, w.Msg)
		}
		if w.Msg.Id != r.Id || !w.Msg.RecursionAvailable || !w.Msg.RecursionDesired || w.Msg.AuthenticatedData {
			t.Fatalf("invalid recursive reply: %v", w.Msg)
		}
		if !reflect.DeepEqual(r.Copy(), original) {
			t.Fatal("SDNS modified the CoreDNS request")
		}
		if name == "example." && (len(w.Msg.Answer) != 1 || w.Msg.Answer[0].(*dns.A).A.String() != "192.0.2.42") {
			t.Fatalf("unexpected answer: %v", w.Msg)
		}
		if name == "alias." && (len(w.Msg.Answer) != 2 || w.Msg.Answer[1].(*dns.A).A.String() != "192.0.2.42") {
			t.Fatalf("incomplete CNAME: %v", w.Msg)
		}
	}
	if calls.Load() == 0 {
		t.Fatal("SDNS never contacted the authority")
	}
	if middleware.Ready() != wasReady {
		t.Fatal("embedding changed the global SDNS pipeline")
	}
}

func TestSDNSCancellation(t *testing.T) {
	// A silent socket exercises cancellation during an actual upstream exchange.
	conn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	engine := newSDNSEngine([]string{conn.LocalAddr().String()}, time.Second)
	defer engine.close()
	r := new(dns.Msg).SetQuestion("example.", dns.TypeA)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := engine.Resolve(ctx, r); !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := engine.Resolve(ctx, r); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error=%v", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("cancellation took %v", elapsed)
	}
}

func TestSDNSConcurrentInstances(t *testing.T) {
	root1, calls1 := testAuthority(t)
	root2, calls2 := testAuthority(t)
	e1 := newSDNSEngine([]string{root1}, time.Second)
	e2 := newSDNSEngine([]string{root2}, time.Second)
	defer e1.close()
	defer e2.close()
	request := new(dns.Msg).SetQuestion("example.", dns.TypeA)
	var wg sync.WaitGroup
	for _, engine := range []*sdnsEngine{e1, e2} {
		for range 8 {
			wg.Add(1)
			go func(e *sdnsEngine) {
				defer wg.Done()
				m, err := e.Resolve(context.Background(), request)
				if err != nil || m == nil || m.Rcode != dns.RcodeSuccess || len(m.Answer) != 1 {
					t.Errorf("concurrent lookup: response=%v err=%v", m, err)
				}
			}(engine)
		}
	}
	wg.Wait()
	if calls1.Load() == 0 || calls2.Load() == 0 {
		t.Fatal("resolver instances did not use their own configured roots")
	}
}

type queryerFunc func(context.Context, *dns.Msg) (*dns.Msg, error)

func (f queryerFunc) Query(ctx context.Context, r *dns.Msg) (*dns.Msg, error) { return f(ctx, r) }

func TestSDNSShutdown(t *testing.T) {
	e := newSDNSEngine(rootHints, time.Second)
	e.pipeline = middleware.NewRegistry().Build(e.cfg)
	started := make(chan struct{})
	e.queryer = queryerFunc(func(ctx context.Context, _ *dns.Msg) (*dns.Msg, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	done := make(chan error, 1)
	r := new(dns.Msg).SetQuestion("example.", dns.TypeA)
	go func() { _, err := e.Resolve(context.Background(), r); done <- err }()
	<-started
	if err := e.close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("in-flight query: %v", err)
	}
	if _, err := e.Resolve(context.Background(), r); !errors.Is(err, context.Canceled) {
		t.Fatalf("query after shutdown: %v", err)
	}
	if err := e.start(); !errors.Is(err, context.Canceled) {
		t.Fatalf("restart after shutdown: %v", err)
	}
}
