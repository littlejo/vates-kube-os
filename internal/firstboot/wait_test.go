package firstboot

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestWaitForIPReturnsImmediatelyWhenTheAddressIsThere(t *testing.T) {
	// The common case: the address is already assigned, so there must be no
	// delay and no log line about waiting.
	var logged int
	ip, err := waitForIP(
		func() (string, error) { return "10.0.2.15", nil },
		"eth0", time.Second,
		func(string, ...any) { logged++ },
	)
	if err != nil {
		t.Fatalf("waitForIP() failed: %v", err)
	}
	if ip != "10.0.2.15" {
		t.Errorf("waitForIP() = %q, want the address", ip)
	}
	if logged != 0 {
		t.Errorf("waitForIP() logged %d line(s) for an address that was already there", logged)
	}
}

func TestWaitForIPWaitsForALateAddress(t *testing.T) {
	// The DHCP case: the address appears after a moment. This must succeed
	// rather than fail the unit on a node that is merely a second early.
	attempts := 0
	var announced bool
	ip, err := waitForIP(
		func() (string, error) {
			attempts++
			if attempts < 3 {
				return "", errors.New("interface eth0 has no IPv4 address yet")
			}
			return "192.168.122.50", nil
		},
		"eth0", 30*time.Second,
		func(f string, a ...any) {
			if strings.Contains(f, "appeared") {
				announced = true
			}
		},
	)
	if err != nil {
		t.Fatalf("waitForIP() gave up on an address that arrived: %v", err)
	}
	if ip != "192.168.122.50" {
		t.Errorf("waitForIP() = %q, want 192.168.122.50", ip)
	}
	if !announced {
		t.Error("waitForIP() did not say that it had waited; a delay should not be silent")
	}
}

func TestWaitForIPFailsAfterTheTimeout(t *testing.T) {
	// A node that never gets an address must fail, and the error must say how
	// long it waited and on which interface -- otherwise the operator is left
	// guessing whether the wait even happened.
	start := time.Now()
	_, err := waitForIP(
		func() (string, error) { return "", errors.New("interface eth0 has no IPv4 address yet") },
		"eth0", 2*time.Second, nil,
	)
	if err == nil {
		t.Fatal("waitForIP() succeeded with an interface that never got an address")
	}
	if !strings.Contains(err.Error(), "eth0") || !strings.Contains(err.Error(), "after") {
		t.Errorf("error was %q, want it to name the interface and the timeout", err)
	}
	if elapsed := time.Since(start); elapsed < 2*time.Second {
		t.Errorf("waitForIP() gave up after %s, before its 2s timeout", elapsed)
	}
}
