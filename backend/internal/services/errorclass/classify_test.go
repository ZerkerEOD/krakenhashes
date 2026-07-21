package errorclass

import "testing"

func TestClassify(t *testing.T) {
	cases := []struct {
		name string
		msg  string
		want Category
	}{
		{"empty", "", CategoryUnknown},
		{"typed no hashes", "BENCHMARK_NO_HASHES_LOADED: hashcat rejected all hashes (exit 255)", CategoryHashlistFatal},
		{"token length", "Token length exception on line 1", CategoryHashlistFatal},
		{"separator", "Separator unmatched in hash file", CategoryHashlistFatal},
		{"typed timeout", "BENCHMARK_TIMEOUT: no status updates received during 120s speed test", CategoryAgentTransient},
		{"oom", "clEnqueueNDRangeKernel(): CL_OUT_OF_RESOURCES", CategoryAgentTransient},
		{"disk full", "write /data/x: no space left on device", CategoryAgentTransient},
		{"watchdog", "GPU watchdog alarm: temperature limit reached", CategoryAgentTransient},
		{"output read", "Output reading failed: read |0: file already closed", CategoryAgentTransient},
		{"no device", "clGetDeviceIDs(): CL_DEVICE_NOT_FOUND No devices found", CategoryAgentPersistent},
		{"no platform", "ATTENTION! No OpenCL, HIP or CUDA compatible platform found", CategoryAgentPersistent},
		{"cuda init", "cuInit(): no CUDA-capable device is detected", CategoryAgentPersistent},
		{"zero speed", "BENCHMARK_ZERO_SPEED: every device reported 0 H/s", CategoryAgentPersistent},
		{"typed autotune retryable", "AGENT_AUTOTUNE: kernel autotune failure skipped the device before any candidates were tested (hashcat exit 0)", CategoryAgentTransient},
		{"typed no work", "AGENT_NO_WORK: hashcat exited 0 without processing any candidates (no status ever reported)", CategoryAgentTransient},
		{"bare autotune stays persistent", "Aborting session due to kernel autotune failures, for all active devices", CategoryAgentPersistent},
		{"invalid mask", "Invalid mask '?z' in mask-file", CategoryJobConfig},
		{"unknown", "something totally unexpected happened", CategoryUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Classify(tc.msg); got != tc.want {
				t.Fatalf("Classify(%q) = %q, want %q", tc.msg, got, tc.want)
			}
		})
	}
}

func TestCategoryIsTransient(t *testing.T) {
	if !CategoryAgentTransient.IsTransient() || !CategoryUnknown.IsTransient() {
		t.Fatal("transient + unknown should be transient")
	}
	if CategoryHashlistFatal.IsTransient() || CategoryJobConfig.IsTransient() || CategoryAgentPersistent.IsTransient() {
		t.Fatal("fatal/config/persistent must not be transient")
	}
}
