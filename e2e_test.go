package recurse

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func startCoreDNS(t *testing.T, options string) (string, string) {
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
			return addr, conf
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("CoreDNS did not start")
	return "", ""
}

func TestCoreDNSEndToEnd(t *testing.T) {
	root, _ := testAuthority(t)
	addr, _ := startCoreDNS(t, " "+testSDNSConfig(t, root))
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
	path := os.Getenv("RECURSE_CONFIG")
	if path == "" {
		path = "examples/sdns.conf"
	}
	addr, _ := startCoreDNS(t, " "+path)
	for _, network := range []string{"udp", "tcp"} {
		r := new(dns.Msg).SetQuestion("example.org.", dns.TypeA)
		r.SetEdns0(1232, true)
		m, _, err := (&dns.Client{Net: network, Timeout: 15 * time.Second}).Exchange(r, addr)
		if err != nil {
			t.Fatal(err)
		}
		if m.Rcode != dns.RcodeSuccess || !m.RecursionAvailable {
			t.Fatalf("%s: %v", network, m)
		}
		if os.Getenv("RECURSE_EXPECT_AD") == "1" && !m.AuthenticatedData {
			t.Fatalf("DNSSEC answer was not authenticated: %v", m)
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
		t.Logf("%s: rcode=%s ad=%t answer=%s", network, dns.RcodeToString[m.Rcode], m.AuthenticatedData, m.Answer)
	}
}

func TestCoreDNSInvalidReload(t *testing.T) {
	root, _ := testAuthority(t)
	sdnsPath := testSDNSConfig(t, root)
	addr, conf := startCoreDNS(t, " "+sdnsPath+"\n reload 2s 0s")
	data, err := os.ReadFile(conf)
	if err != nil {
		t.Fatal(err)
	}
	// A syntactically valid Corefile rejected during plugin setup makes Caddy
	// attempt a restart; the previous resolver must stay alive afterward.
	invalid := strings.Replace(string(data), sdnsPath, sdnsPath+".missing", 1)
	if err := os.WriteFile(conf, []byte(invalid), 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	rejected := false
	for time.Now().Before(deadline) {
		log, _ := os.ReadFile(filepath.Join(filepath.Dir(conf), "coredns.log"))
		if strings.Contains(string(log), "reload failed") {
			rejected = true
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if !rejected {
		t.Fatal("CoreDNS did not attempt and reject the invalid reload")
	}
	r := new(dns.Msg).SetQuestion("example.", dns.TypeA)
	m, _, err := (&dns.Client{Timeout: time.Second}).Exchange(r, addr)
	if err != nil || m == nil || m.Rcode != dns.RcodeSuccess || len(m.Answer) != 1 {
		t.Fatalf("old resolver stopped serving after rejected reload: %v %v", m, err)
	}
}

func testSDNSConfig(t *testing.T, root string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "sdns.conf")
	data := fmt.Sprintf("directory = %q\ndnssec = \"off\"\nrootservers = [%q]\nquerytimeout = \"2s\"\ncachesize = 1024\n", dir, root)
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
