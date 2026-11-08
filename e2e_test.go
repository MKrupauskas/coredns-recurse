package recurse

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func startCoreDNS(t *testing.T, options string) string {
	t.Helper()
	binary := os.Getenv("COREDNS_BINARY")
	if binary == "" {
		t.Skip("set COREDNS_BINARY to run tests against a compiled CoreDNS")
	}
	binary, err := filepath.Abs(binary)
	if err != nil {
		t.Fatal(err)
	}
	// Reserve a candidate port, then release it for the child process.
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	_ = l.Close()
	dir := t.TempDir()
	conf := filepath.Join(dir, "Corefile")
	corefile := fmt.Sprintf(".:%s {\n bind 127.0.0.1\n errors\n recurse%s\n}\n", port, options)
	if err := os.WriteFile(conf, []byte(corefile), 0600); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "coredns.log")
	log, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-conf", conf)
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		_ = log.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Signal(os.Interrupt)
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("CoreDNS exited: %v", err)
			}
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			<-done
			t.Error("CoreDNS did not shut down gracefully")
		}
		_ = log.Close()
		if t.Failed() {
			data, _ := os.ReadFile(logPath)
			t.Logf("CoreDNS log:\n%s", data)
		}
	})
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return addr
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("CoreDNS did not start")
	return ""
}

func TestCoreDNSEndToEnd(t *testing.T) {
	root, _ := testAuthority(t)
	addr := startCoreDNS(t, fmt.Sprintf(" {\n root %s\n timeout 2s\n }", root))
	for _, network := range []string{"udp", "tcp"} {
		r := new(dns.Msg).SetQuestion("example.", dns.TypeA)
		m, _, err := (&dns.Client{Net: network, Timeout: 3 * time.Second}).Exchange(r, addr)
		if err != nil {
			t.Fatal(err)
		}
		if m.Rcode != dns.RcodeSuccess || m.Id != r.Id || !m.RecursionAvailable || len(m.Answer) != 1 {
			t.Fatalf("%s: %v", network, m)
		}
		if a, ok := m.Answer[0].(*dns.A); !ok || a.A.String() != "192.0.2.42" {
			t.Fatalf("%s: %v", network, m)
		}
	}
}

func TestCoreDNSPublicRecursion(t *testing.T) {
	if os.Getenv("RECURSE_LIVE_TEST") != "1" {
		t.Skip("set RECURSE_LIVE_TEST=1 to query public DNS authorities")
	}
	addr := startCoreDNS(t, "")
	for _, network := range []string{"udp", "tcp"} {
		r := new(dns.Msg).SetQuestion("example.org.", dns.TypeA)
		m, _, err := (&dns.Client{Net: network, Timeout: 15 * time.Second}).Exchange(r, addr)
		if err != nil {
			t.Fatal(err)
		}
		if m.Rcode != dns.RcodeSuccess || !m.RecursionAvailable || m.AuthenticatedData {
			t.Fatalf("%s: %v", network, m)
		}
		found := false
		for _, rr := range m.Answer {
			if _, ok := rr.(*dns.A); ok {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s: no A answer: %v", network, m)
		}
		t.Logf("%s: %s", network, m.Answer)
	}
}
