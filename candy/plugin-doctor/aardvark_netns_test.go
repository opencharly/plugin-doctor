package doctor

import (
	"strings"
	"testing"
)

// The failure this guards against is silent: aardvark-dns is alive, the bridge exists, and the
// zone file is correct and current -- but the daemon serves a network namespace podman no
// longer uses, so `getent hosts <peer>` returns empty inside every container. A presence check
// reports OK throughout. Only comparing namespaces catches it.
func TestAardvarkStrandedIsReportedMissing(t *testing.T) {
	got := aardvarkVerdict("1906277", "net:[4026534654]", "net:[4026533620]")
	if got.Status != CheckMissing {
		t.Fatalf("a stranded aardvark must not report OK; got %v (%s)", got.Status, got.Detail)
	}
	for _, want := range []string{"STRANDED", "4026534654", "4026533620"} {
		if !strings.Contains(got.Detail, want) {
			t.Errorf("detail must name %q so the operator can see both namespaces; got: %s", want, got.Detail)
		}
	}
	// The hint has to be actionable: killing the process alone is not enough, because the
	// stale pidfile makes podman believe a healthy daemon is already running.
	for _, want := range []string{"kill", "aardvark.pid", "podman network reload"} {
		if !strings.Contains(got.InstallHint, want) {
			t.Errorf("hint must include %q; got: %s", want, got.InstallHint)
		}
	}
}

// Matching namespaces are the healthy case and must stay quiet, or the check becomes noise
// that operators learn to ignore.
func TestAardvarkInCurrentNamespaceIsOK(t *testing.T) {
	got := aardvarkVerdict("2451338", "net:[4026533620]", "net:[4026533620]")
	if got.Status != CheckOK {
		t.Fatalf("matching namespaces must be OK; got %v (%s)", got.Status, got.Detail)
	}
	if strings.Contains(got.Detail, "STRANDED") {
		t.Errorf("healthy detail must not say STRANDED: %s", got.Detail)
	}
}

// aardvark-dns not running is NOT a fault: podman starts it on demand with the first
// container. Reporting it as broken on an idle host would be a false alarm.
func TestAardvarkNotRunningIsOK(t *testing.T) {
	got := aardvarkVerdict("", "", "net:[4026533620]")
	if got.Status != CheckOK {
		t.Fatalf("absent aardvark must be OK on an idle host; got %v (%s)", got.Status, got.Detail)
	}
}

// If the comparison cannot be made, the check must not invent a verdict in either direction.
func TestAardvarkUnknownNamespaceIsNotAFailure(t *testing.T) {
	got := aardvarkVerdict("1906277", "net:[4026534654]", "")
	if got.Status != CheckOK {
		t.Fatalf("an unavailable comparison must not be reported as a fault; got %v", got.Status)
	}
}
