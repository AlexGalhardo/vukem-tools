package devicelink_test

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/AlexGalhardo/vukem/cache"

	"github.com/AlexGalhardo/vukem-tools/app/abilities"
	"github.com/AlexGalhardo/vukem-tools/app/devicelink"
)

type clock struct {
	mu sync.Mutex
	at time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.at
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.at = c.at.Add(d)
}

func newService() (*devicelink.Service, *clock) {
	clk := &clock{at: time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)}

	return devicelink.New(cache.NewMemory(100), clk.now), clk
}

func TestALinkIsApprovedByAPersonAndClaimedOnceByTheProgram(t *testing.T) {
	t.Parallel()

	svc, _ := newService()
	ctx := context.Background()

	started, err := svc.Start(ctx, devicelink.Telegram, "Telegram")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	if !regexp.MustCompile(`^[BCDFGHJKMNPQRTVWXZ2-9]{4}-[BCDFGHJKMNPQRTVWXZ2-9]{4}$`).MatchString(started.UserCode) {
		t.Errorf("user code %q is not XXXX-XXXX from the unambiguous alphabet", started.UserCode)
	}

	if started.DeviceCode == "" || started.DeviceCode == started.UserCode {
		t.Fatal("the device code must be a separate secret")
	}

	if out, _ := svc.Poll(ctx, started.DeviceCode); out.Status != devicelink.Pending {
		t.Fatalf("before approval the poll says %q, want pending", out.Status)
	}

	// The person types the code however they like.
	req, ok, err := svc.Lookup(ctx, " "+lower(started.UserCode)+" ")
	if err != nil || !ok {
		t.Fatalf("Lookup = %v, %v; want the pending request", ok, err)
	}

	// The abilities come from the server's table, not from the program.
	if !slices.Equal(req.Abilities, devicelink.Abilities(devicelink.Telegram)) {
		t.Errorf("abilities = %v, want the telegram set", req.Abilities)
	}

	for _, a := range req.Abilities {
		if !abilities.Valid(a) {
			t.Errorf("the telegram set grants %q, which app/abilities does not define", a)
		}
	}

	if err := svc.Decide(ctx, started.UserCode, "user-1", true); err != nil {
		t.Fatalf("Decide: %v", err)
	}

	out, err := svc.Poll(ctx, started.DeviceCode)
	if err != nil || out.Status != devicelink.Approved || out.UserID != "user-1" {
		t.Fatalf("first poll = %+v, %v; want approved for user-1", out, err)
	}

	// The link is spent: a second poll must not mint a second key.
	if again, _ := svc.Poll(ctx, started.DeviceCode); again.Status == devicelink.Approved {
		t.Error("a spent link was claimed twice")
	}
}

func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}

	return string(b)
}

func TestOnlyOnePollerWinsAnApprovedLink(t *testing.T) {
	t.Parallel()

	svc, _ := newService()
	ctx := context.Background()
	started, _ := svc.Start(ctx, devicelink.Telegram, "Telegram")

	if err := svc.Decide(ctx, started.UserCode, "user-1", true); err != nil {
		t.Fatalf("Decide: %v", err)
	}

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		wins int
	)

	for range 20 {
		wg.Go(func() {
			if out, _ := svc.Poll(ctx, started.DeviceCode); out.Status == devicelink.Approved {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		})
	}

	wg.Wait()

	if wins != 1 {
		t.Fatalf("%d pollers were handed the approval, want exactly 1", wins)
	}
}

func TestADeniedOrExpiredLinkGivesNothing(t *testing.T) {
	t.Parallel()

	svc, clk := newService()
	ctx := context.Background()

	denied, _ := svc.Start(ctx, devicelink.Telegram, "")
	if err := svc.Decide(ctx, denied.UserCode, "user-1", false); err != nil {
		t.Fatalf("Decide: %v", err)
	}

	if out, _ := svc.Poll(ctx, denied.DeviceCode); out.Status != devicelink.Denied || out.UserID != "" {
		t.Errorf("denied poll = %+v", out)
	}

	late, _ := svc.Start(ctx, devicelink.Telegram, "")

	clk.advance(devicelink.TTL + time.Second)

	if _, ok, _ := svc.Lookup(ctx, late.UserCode); ok {
		t.Error("an expired link can still be looked up")
	}

	if err := svc.Decide(ctx, late.UserCode, "user-1", true); !errors.Is(err, devicelink.ErrUnknown) {
		t.Errorf("approving an expired link: err = %v, want ErrUnknown", err)
	}

	if out, _ := svc.Poll(ctx, late.DeviceCode); out.Status != devicelink.Expired {
		t.Errorf("expired poll says %q", out.Status)
	}
}

func TestGuessesAndBlanksAreIndistinguishableFromExpiry(t *testing.T) {
	t.Parallel()

	svc, _ := newService()
	ctx := context.Background()

	for _, device := range []string{"", "nope", string(make([]byte, 500))} {
		if out, err := svc.Poll(ctx, device); err != nil || out.Status != devicelink.Expired {
			t.Errorf("Poll(%.10q) = %+v, %v; want expired", device, out, err)
		}
	}

	if _, ok, _ := svc.Lookup(ctx, "AAAA-AAAA"); ok {
		t.Error("an invented user code was found")
	}

	if _, err := svc.Start(ctx, devicelink.Client("other"), ""); err == nil {
		t.Error("an unknown client kind was accepted")
	}
}
