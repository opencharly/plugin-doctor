package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The failure this group exists for is a spec that declares devices but no driver mounts: the
// container then resolves libnvidia-*/libcuda from its own image and an image at a different driver
// version shadows the host driver (opencharly/layer-cuda#21). Both shapes are fixtures here so the
// check cannot silently pass on the broken one.
const cdiSpecWithMounts = `cdiVersion: 0.7.0
kind: nvidia.com/gpu
devices:
  - name: all
    containerEdits:
      mounts:
        - hostPath: /usr/lib/libnvidia-ml.so.615.78.08
          containerPath: /usr/lib/libnvidia-ml.so.615.78.08
        - hostPath: /usr/lib/libcuda.so.615.78.08
          containerPath: /usr/lib/libcuda.so.615.78.08
        - hostPath: /usr/bin/nvidia-smi
          containerPath: /usr/bin/nvidia-smi
`

const cdiSpecWithoutMounts = `cdiVersion: 0.7.0
kind: nvidia.com/gpu
devices:
  - name: "0"
    containerEdits: {}
  - name: all
    containerEdits: {}
`

func writeTemp(t *testing.T, name, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSummariseCDICountsMountsAndDriverLibs(t *testing.T) {
	sum, ok := readCDISpec([]string{writeTemp(t, "nvidia.yaml", cdiSpecWithMounts)})
	if !ok {
		t.Fatal("spec not found")
	}
	if sum.DeviceCount != 1 {
		t.Errorf("devices = %d, want 1", sum.DeviceCount)
	}
	if sum.MountCount != 3 {
		t.Errorf("mounts = %d, want 3", sum.MountCount)
	}
	if len(sum.DriverLibs) != 2 {
		t.Errorf("driver libs = %v, want 2 entries", sum.DriverLibs)
	}
	// versionOfSuffix is the live path: a versioned name yields its version, a SONAME-only name
	// yields nothing — comparing against "1" would be comparing against nothing.
	if got := versionOfSuffix("libnvidia-ml.so.615.78.08"); got != "615.78.08" {
		t.Errorf("version from a versioned lib = %q, want 615.78.08", got)
	}
	if got := versionOfSuffix("libnvidia-ml.so.1"); got != "" {
		t.Errorf("version from a SONAME-only name = %q, want empty", got)
	}
}

func TestReadCDISpecReportsEmptySpec(t *testing.T) {
	sum, ok := readCDISpec([]string{writeTemp(t, "nvidia.yaml", cdiSpecWithoutMounts)})
	if !ok {
		t.Fatal("spec not found")
	}
	if sum.MountCount != 0 {
		t.Errorf("mounts = %d, want 0 for the measured broken shape", sum.MountCount)
	}
	if len(sum.DriverLibs) != 0 {
		t.Errorf("driver libs = %v, want none", sum.DriverLibs)
	}
}

func TestReadCDISpecMissingFile(t *testing.T) {
	if _, ok := readCDISpec([]string{filepath.Join(t.TempDir(), "absent.yaml")}); ok {
		t.Fatal("a missing spec must report not-found")
	}
}

func TestKernelModuleVersion(t *testing.T) {
	p := writeTemp(t, "version",
		"NVRM version: NVIDIA UNIX Open Kernel Module for x86_64  615.78.08  Release Build  (root@a)  \n")
	if got := kernelModuleVersion(p); got != "615.78.08" {
		t.Errorf("kernel module = %q, want 615.78.08", got)
	}
	if got := kernelModuleVersion(filepath.Join(t.TempDir(), "absent")); got != "" {
		t.Errorf("unreadable /proc must yield empty, got %q", got)
	}
}

// The verdicts: a broken spec must WARN (never OK), and a spec whose userspace differs from the
// running kernel module must WARN with both versions named.
func TestCDIChecksWarnOnBrokenShapes(t *testing.T) {
	savedSpec, savedProc, savedLook, savedLib := cdiSpecPaths, procDriverVersionPath, execLookPath, hostLibDirPath
	defer func() {
		cdiSpecPaths, procDriverVersionPath, execLookPath, hostLibDirPath = savedSpec, savedProc, savedLook, savedLib
	}()

	execLookPath = func(string) (string, error) { return "/usr/bin/nvidia-ctk", nil }
	procDriverVersionPath = writeTemp(t, "version", "NVRM version: ... 615.78.08 Release Build\n")

	// (a) spec with no mounts, BOTH directions, forced through the seam so the verdict can never
	// depend on what the test host happens to have configured.
	savedHook := runtimeHookDetect
	defer func() { runtimeHookDetect = savedHook }()
	runtimeHookDetect = func() bool { return false }
	cdiSpecPaths = []string{writeTemp(t, "nvidia.yaml", cdiSpecWithoutMounts)}
	checks := cdiChecks(Distro{})
	var specCheck *DoctorCheckResult
	for i := range checks {
		if checks[i].Name == "CDI spec" {
			specCheck = &checks[i]
		}
	}
	if specCheck == nil {
		t.Fatal("no CDI spec check emitted")
	}
	// No hook and no mounts: the check MUST warn, and name the mechanism.
	if specCheck.Status != CheckWarning {
		t.Errorf("no hook + 0 mounts = %v, want CheckWarning (detail: %q)", specCheck.Status, specCheck.Detail)
	}
	if !strings.Contains(specCheck.Detail, "NO driver mounts") {
		t.Errorf("warning must name the mechanism, got %q", specCheck.Detail)
	}
	// With a hook present the same spec is a datum, and the detail must still name the hook.
	runtimeHookDetect = func() bool { return true }
	checks = cdiChecks(Distro{})
	for _, c := range checks {
		if c.Name == "CDI spec" {
			if c.Status != CheckOK {
				t.Errorf("hook + 0 mounts = %v, want CheckOK (detail: %q)", c.Status, c.Detail)
			}
			if !strings.Contains(c.Detail, "runtime hook") {
				t.Errorf("OK verdict must name the hook that supplies the driver, got %q", c.Detail)
			}
		}
	}

	// (b) version skew: the HOST's own userspace is 620.70.06 while the module is 615.78.08. That
	// pair is what the injection carries, so it is what this check compares.
	libDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(libDir, "libnvidia-ml.so.620.70.06"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	hostLibDirPath = libDir
	cdiSpecPaths = []string{writeTemp(t, "nvidia.yaml", cdiSpecWithMounts)}
	checks = cdiChecks(Distro{})
	var verCheck *DoctorCheckResult
	for i := range checks {
		if checks[i].Name == "CDI driver version" {
			verCheck = &checks[i]
		}
	}
	if verCheck == nil {
		t.Fatal("no CDI driver version check emitted")
	}
	if verCheck.Status != CheckWarning {
		t.Errorf("version skew = %v, want CheckWarning", verCheck.Status)
	}
	if !strings.Contains(verCheck.Detail, "620.70.06") || !strings.Contains(verCheck.Detail, "615.78.08") {
		t.Errorf("detail must name both versions, got %q", verCheck.Detail)
	}

	// (c) the healthy shape is OK: host userspace and module agree.
	hostLibDirPath = t.TempDir()
	if err := os.WriteFile(filepath.Join(hostLibDirPath, "libnvidia-ml.so.615.78.08"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	cdiSpecPaths = []string{writeTemp(t, "nvidia.yaml", cdiSpecWithMounts)}
	checks = cdiChecks(Distro{})
	for _, c := range checks {
		if c.Name == "CDI driver version" && c.Status != CheckOK {
			t.Errorf("matching versions = %v, want CheckOK (%s)", c.Status, c.Detail)
		}
		if c.Name == "CDI spec" && c.Status != CheckOK {
			t.Errorf("spec with mounts = %v, want CheckOK (%s)", c.Status, c.Detail)
		}
	}
}
