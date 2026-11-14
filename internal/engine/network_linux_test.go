//go:build linux

package engine_test

import (
	"errors"
	"net"
	"testing"
	"time"

	"github.com/AlexGalhardo/vukem-tools/internal/engine"
)

// listener is a TCP server on the loopback of the test's own network
// namespace, which is exactly what a program in another namespace must not
// reach.
func listener(t *testing.T) string {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	t.Cleanup(func() { _ = l.Close() })

	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}

			_ = conn.Close()
		}
	}()

	return l.Addr().String()
}

// isolatingRunner is a production runner that insists on a network namespace,
// or skips where the host does not grant one (an unprivileged container with
// the default seccomp profile, a CI runner that restricts user namespaces).
func isolatingRunner(t *testing.T) *engine.Runner {
	t.Helper()

	r, err := engine.NewRunner(engine.RunnerOptions{
		Limits:     engine.Limits{Timeout: 10 * time.Second},
		Production: true,
	})
	if err != nil {
		t.Skipf("this host cannot create a network namespace: %v", err)
	}

	return r
}

func TestAnEngineCannotReachTheNetwork(t *testing.T) {
	t.Parallel()

	addr := listener(t)
	r := isolatingRunner(t)

	_, err := run(t, r, "dial", engine.Command{Env: []string{"ENGINE_TEST_ADDR=" + addr}})

	var exit *engine.ExitError
	if !errors.As(err, &exit) || exit.Code != 7 {
		t.Fatalf("the engine reached %s from its own network namespace (err: %v)", addr, err)
	}
}

func TestTheDownloaderKeepsTheNetwork(t *testing.T) {
	t.Parallel()

	addr := listener(t)
	r := isolatingRunner(t)

	_, err := run(t, r, "dial", engine.Command{
		Network: true,
		Env:     []string{"ENGINE_TEST_ADDR=" + addr},
	})
	if err != nil {
		t.Fatalf("a command that asked for the network could not reach %s: %v", addr, err)
	}
}

// Without the option a production runner on a host that cannot isolate must
// refuse to start rather than run engines with the network.
func TestProductionFailsClosedWithoutANetworkNamespace(t *testing.T) {
	t.Parallel()

	opts := engine.RunnerOptions{Limits: engine.Limits{Timeout: time.Second}, Production: true}

	if _, err := engine.NewRunner(opts); err == nil {
		t.Skip("this host can create a network namespace, so there is nothing to refuse")
	}

	opts.InsecureAllowNetwork = true

	if _, err := engine.NewRunner(opts); err != nil {
		t.Fatalf("the explicit insecure option did not let the runner start: %v", err)
	}
}
