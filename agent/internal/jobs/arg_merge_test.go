package jobs

import (
	"testing"
)

func TestMergeHashcatArgs(t *testing.T) {
	tests := []struct {
		name      string
		jobArgs   string
		agentArgs string
		want      string
	}{
		{
			name:      "both empty",
			jobArgs:   "",
			agentArgs: "",
			want:      "",
		},
		{
			name:      "job args only",
			jobArgs:   "-w 4 --force",
			agentArgs: "",
			want:      "-w 4 --force",
		},
		{
			name:      "agent args only",
			jobArgs:   "",
			agentArgs: "-w 3 -O",
			want:      "-w 3 -O",
		},
		{
			name:      "no conflict - both preserved",
			jobArgs:   "--force",
			agentArgs: "-w 3",
			want:      "--force -w 3",
		},
		{
			name:      "short form conflict - agent wins",
			jobArgs:   "-w 4",
			agentArgs: "-w 3",
			want:      "-w 3",
		},
		{
			name:      "short job vs long agent - agent wins",
			jobArgs:   "-w 4",
			agentArgs: "--workload-profile 3",
			want:      "--workload-profile 3",
		},
		{
			name:      "long job vs short agent - agent wins",
			jobArgs:   "--workload-profile 4",
			agentArgs: "-w 3",
			want:      "-w 3",
		},
		{
			name:      "long equals job vs short agent - agent wins",
			jobArgs:   "--workload-profile=4",
			agentArgs: "-w 3",
			want:      "-w 3",
		},
		{
			name:      "boolean flag dedup - appears once",
			jobArgs:   "--force",
			agentArgs: "--force",
			want:      "--force",
		},
		{
			name:      "boolean short form dedup",
			jobArgs:   "-O",
			agentArgs: "-O",
			want:      "-O",
		},
		{
			name:      "mixed conflict and non-conflict",
			jobArgs:   "-w 4 --force --status-timer 5",
			agentArgs: "-w 3 --self-test-disable",
			want:      "-w 3 --force --status-timer 5 --self-test-disable",
		},
		{
			name:      "status-timer conflict with equals syntax",
			jobArgs:   "--status-timer=5",
			agentArgs: "--status-timer=10",
			want:      "--status-timer=10",
		},
		{
			name:      "status-timer conflict - space vs equals",
			jobArgs:   "--status-timer 5",
			agentArgs: "--status-timer=10",
			want:      "--status-timer=10",
		},
		{
			name:      "multiple non-conflicting from both",
			jobArgs:   "--force --hex-charset",
			agentArgs: "-w 3 -O",
			want:      "--force --hex-charset -w 3 -O",
		},
		{
			name:      "kernel threads conflict",
			jobArgs:   "-T 32",
			agentArgs: "-T 64",
			want:      "-T 64",
		},
		{
			name:      "kernel threads short vs long",
			jobArgs:   "-T 32",
			agentArgs: "--kernel-threads 64",
			want:      "--kernel-threads 64",
		},
		{
			name:      "complex real-world scenario",
			jobArgs:   "-w 4 --force --runtime 3600 --markov-threshold 50",
			agentArgs: "-w 3 -O",
			want:      "-w 3 --force --runtime 3600 --markov-threshold 50 -O",
		},
		{
			name:      "hwmon-temp-abort conflict",
			jobArgs:   "--hwmon-temp-abort 90",
			agentArgs: "--hwmon-temp-abort 100",
			want:      "--hwmon-temp-abort 100",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MergeHashcatArgs(tt.jobArgs, tt.agentArgs)
			if got != tt.want {
				t.Errorf("MergeHashcatArgs(%q, %q) = %q, want %q", tt.jobArgs, tt.agentArgs, got, tt.want)
			}
		})
	}
}

func TestParseArgs(t *testing.T) {
	tests := []struct {
		name  string
		args  string
		count int
	}{
		{"empty", "", 0},
		{"single boolean", "--force", 1},
		{"single short bool", "-O", 1},
		{"short with value", "-w 4", 1},
		{"long with equals", "--status-timer=5", 1},
		{"long with space", "--workload-profile 4", 1},
		{"multiple mixed", "-w 4 --force -O --status-timer=5", 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseArgs(tt.args)
			if len(result) != tt.count {
				t.Errorf("parseArgs(%q) returned %d args, want %d", tt.args, len(result), tt.count)
			}
		})
	}
}

func TestCanonicalize(t *testing.T) {
	tests := []struct {
		flag string
		want string
	}{
		{"-w", "--workload-profile"},
		{"-O", "--optimized-kernel-enable"},
		{"-T", "--kernel-threads"},
		{"-t", "--markov-threshold"},
		{"--force", "--force"},           // No mapping, returns as-is
		{"--unknown", "--unknown"},       // Unknown, returns as-is
	}

	for _, tt := range tests {
		t.Run(tt.flag, func(t *testing.T) {
			got := canonicalize(tt.flag)
			if got != tt.want {
				t.Errorf("canonicalize(%q) = %q, want %q", tt.flag, got, tt.want)
			}
		})
	}
}
