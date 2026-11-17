package doctor

// CDI (Container Device Interface) health for NVIDIA GPU hosts.
//
// WHY THIS EXISTS. The NVIDIA CDI injection is the ONE supported source of the driver user-space a
// GPU container resolves: the kernel module is host-global, so userspace must come from the host at
// the host's version. That contract is invisible unless it is checked, and it fails quietly in two
// ways that this group is built to catch:
//
//  1. A CDI spec that declares devices but NO driver mounts. Measured on a real host: both
//     /etc/cdi/nvidia.yaml and /var/run/cdi/nvidia.yaml declared three devices, three deviceNodes
//     and zero mounts, so nothing in the spec supplied a library. A container then resolves
//     libnvidia-*/libcuda from whatever its IMAGE ships.
//
//  2. An image that ships its own driver user-space at a different version than the host. The
//     injection mounts the host's VERSIONED files (libnvidia-ml.so.615.78.08, ...), the image's own
//     libnvidia-ml.so.620.70.06 stays visible next to them, and the SONAME link resolves to the
//     newest file present — the baked one. nvidia-smi then prints "Driver/library version mismatch"
//     and exits 18, and any consumer that estimates free memory through NVML (ollama among them)
//     silently degrades to CPU. That is opencharly/layer-cuda#21.
//
// So the group reports three things that must all hold for a GPU container to use the GPU:
// the toolkit that generates the spec, a spec that actually mounts the driver, and a userspace
// version equal to the running kernel module's.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// cdiSpecPaths are the two directories the container runtimes read CDI specs from. /etc/cdi is the
// persistent one; /var/run/cdi is where `nvidia-ctk cdi generate` writes by convention and is
// regenerated per boot, so a host that only ever wrote /etc/cdi keeps a stale spec there.
var cdiSpecPaths = []string{
	"/etc/cdi/nvidia.yaml",
	"/var/run/cdi/nvidia.yaml",
}

// procDriverVersionPath is a package var so tests can point it at a fixture.
var procDriverVersionPath = "/proc/driver/nvidia/version"

// cdiSpec is the slice of a CDI spec this check reads.
type cdiSpec struct {
	CDIVersion string `yaml:"cdiVersion"`
	Kind       string `yaml:"kind"`
	Devices    []struct {
		Name           string `yaml:"name"`
		ContainerEdits struct {
			Mounts []struct {
				HostPath      string `yaml:"hostPath"`
				ContainerPath string `yaml:"containerPath"`
			} `yaml:"mounts"`
		} `yaml:"containerEdits"`
	} `yaml:"devices"`
}

// cdiSummary is what the checks reason about, separated from the file read so the parsing is
// testable without a host.
type cdiSummary struct {
	Path        string
	Kind        string
	DeviceCount int
	MountCount  int      // across every device
	DriverLibs  []string // versioned driver libraries the spec mounts, if any
}

// summariseCDI counts what a parsed spec actually provides. A spec that mounts nothing is the
// failure mode that makes the image the only provider.
func summariseCDI(path string, spec cdiSpec) cdiSummary {
	s := cdiSummary{Path: path, Kind: spec.Kind, DeviceCount: len(spec.Devices)}
	seen := map[string]bool{}
	for _, d := range spec.Devices {
		s.MountCount += len(d.ContainerEdits.Mounts)
		for _, m := range d.ContainerEdits.Mounts {
			base := filepath.Base(m.HostPath)
			if isDriverLib(base) && !seen[base] {
				seen[base] = true
				s.DriverLibs = append(s.DriverLibs, base)
			}
		}
	}
	return s
}

// isDriverLib reports whether a basename is one of the host-coupled driver libraries the injection
// is supposed to carry. These are exactly the names an image must never ship.
func isDriverLib(base string) bool {
	for _, p := range []string{"libnvidia-", "libcuda", "libnvcuvid", "libnvoptix", "libGLX_nvidia", "libEGL_nvidia", "libGLESv"} {
		if strings.HasPrefix(base, p) {
			return true
		}
	}
	return false
}

var versionRe = regexp.MustCompile(`\d+\.\d+(?:\.\d+)?`)

// driverVersionOfLib pulls "615.78.08" out of "libnvidia-ml.so.615.78.08".
func driverVersionOfLib(base string) string {
	return versionRe.FindString(strings.TrimPrefix(base, "libnvidia-"))
}

// kernelModuleVersion reports the running NVIDIA kernel module's version. /proc is the host's, so
// this is the authority the container's userspace must match.
func kernelModuleVersion(procPath string) string {
	b, err := os.ReadFile(procPath)
	if err != nil {
		return ""
	}
	// NVRM version: NVIDIA UNIX Open Kernel Module for x86_64  615.78.08  Release Build ...
	return versionRe.FindString(string(b))
}

// readCDISpec loads the first spec that exists. An absent spec is not an error here; it is reported
// as "not found" by the caller.
func readCDISpec(paths []string) (cdiSummary, bool) {
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var spec cdiSpec
		if err := yaml.Unmarshal(b, &spec); err != nil {
			// An unparseable spec is still a finding: report it with zero counts and let the
			// caller say so, rather than pretending the host has no CDI.
			return cdiSummary{Path: p}, true
		}
		return summariseCDI(p, spec), true
	}
	return cdiSummary{}, false
}

// cdiChecks is the doctor group for a GPU host that deploys containers through CDI.
func cdiChecks(distro Distro) []DoctorCheckResult {
	checks := []DoctorCheckResult{}

	// 1. The toolkit that GENERATES the spec. It is host tooling: an image never needs it, and a
	// host without it has no spec to inject.
	if path, err := lookPathAny("nvidia-ctk", "nvidia-container-toolkit", "nvidia-cdi-hook"); err != nil {
		checks = append(checks, DoctorCheckResult{
			Name:        "NVIDIA container toolkit",
			Status:      CheckWarning,
			Detail:      "no nvidia-ctk / nvidia-container-toolkit on PATH, so no CDI spec can be generated or refreshed",
			InstallHint: distro.installHint("nvidia-container-toolkit"),
		})
	} else {
		checks = append(checks, DoctorCheckResult{
			Name:   "NVIDIA container toolkit",
			Status: CheckOK,
			Detail: path,
		})
	}

	// 2. The spec, and whether it actually mounts the driver.
	sum, found := readCDISpec(cdiSpecPaths)
	switch {
	case !found:
		checks = append(checks, DoctorCheckResult{
			Name:        "CDI spec",
			Status:      CheckWarning,
			Detail:      "no NVIDIA CDI spec in /etc/cdi or /var/run/cdi, so --device nvidia.com/gpu=all cannot resolve",
			InstallHint: "sudo nvidia-ctk cdi generate --output=/var/run/cdi/nvidia.yaml",
		})
	case sum.DeviceCount == 0:
		checks = append(checks, DoctorCheckResult{
			Name:        "CDI spec",
			Status:      CheckWarning,
			Detail:      fmt.Sprintf("%s declares no devices", sum.Path),
			InstallHint: "sudo nvidia-ctk cdi generate --output=/var/run/cdi/nvidia.yaml",
		})
	case sum.MountCount == 0:
		// THE case measured on this host, and the one that costs a GPU: the spec injects device
		// nodes only, so every driver library a container resolves comes from its image.
		checks = append(checks, DoctorCheckResult{
			Name:   "CDI spec",
			Status: CheckWarning,
			Detail: fmt.Sprintf("%s declares %d device(s) but NO driver mounts — containers get device nodes only and resolve libnvidia-*/libcuda from their own image, so an image at a different driver version shadows the host driver (nvidia-smi: Driver/library version mismatch; NVML consumers silently fall back to CPU)", sum.Path, sum.DeviceCount),
			InstallHint: "sudo nvidia-ctk cdi generate --output=/var/run/cdi/nvidia.yaml " +
				"(and check the generator can see the driver root)",
		})
	default:
		checks = append(checks, DoctorCheckResult{
			Name:   "CDI spec",
			Status: CheckOK,
			Detail: fmt.Sprintf("%s: kind=%s, %d device(s), %d mount(s), %d driver librar(y|ies)", sum.Path, sum.Kind, sum.DeviceCount, sum.MountCount, len(sum.DriverLibs)),
		})
	}

	// 3. Userspace vs kernel module. This is the check that fails on the shadowing bug: the
	// version the spec mounts must equal the running module's, because the module is global and
	// userspace must match it.
	km := kernelModuleVersion(procDriverVersionPath)
	uv := ""
	if len(sum.DriverLibs) > 0 {
		uv = driverVersionOfLib(sum.DriverLibs[0])
	}
	switch {
	case km == "":
		checks = append(checks, DoctorCheckResult{
			Name:   "CDI driver version",
			Status: CheckWarning,
			Detail: "/proc/driver/nvidia/version is unreadable, so the host's running driver version cannot be established",
		})
	case uv == "":
		checks = append(checks, DoctorCheckResult{
			Name:   "CDI driver version",
			Status: CheckWarning,
			Detail: fmt.Sprintf("kernel module is %s but the spec mounts no versioned driver library to compare against", km),
		})
	case uv != km:
		checks = append(checks, DoctorCheckResult{
			Name:        "CDI driver version",
			Status:      CheckWarning,
			Detail:      fmt.Sprintf("the spec mounts userspace %s while the running kernel module is %s — containers built from an image at either version will report a driver/library mismatch", uv, km),
			InstallHint: "sudo nvidia-ctk cdi generate --output=/var/run/cdi/nvidia.yaml",
		})
	default:
		checks = append(checks, DoctorCheckResult{
			Name:   "CDI driver version",
			Status: CheckOK,
			Detail: fmt.Sprintf("userspace %s == kernel module %s", uv, km),
		})
	}

	return checks
}

// lookPathAny returns the first of names resolvable on PATH.
func lookPathAny(names ...string) (string, error) {
	var lastErr error
	for _, n := range names {
		if p, err := execLookPath(n); err == nil {
			return p, nil
		} else {
			lastErr = err
		}
	}
	return "", lastErr
}
