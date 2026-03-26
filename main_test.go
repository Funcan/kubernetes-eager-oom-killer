package main

import (
	"testing"
)

func TestParseContainerMemory(t *testing.T) {
	metrics := `# HELP container_memory_working_set_bytes Current working set of the container in bytes
# TYPE container_memory_working_set_bytes gauge
container_memory_working_set_bytes{container="nginx",namespace="default",pod="nginx-abc123"} 5.24288e+07
container_memory_working_set_bytes{container="sidecar",namespace="default",pod="nginx-abc123"} 1048576
container_memory_working_set_bytes{container="app",namespace="kube-system",pod="coredns-xyz"} 20971520
`

	result := parseContainerMemory(metrics)

	tests := []struct {
		key  containerKey
		want int64
	}{
		{containerKey{"default", "nginx-abc123", "nginx"}, 52428800},
		{containerKey{"default", "nginx-abc123", "sidecar"}, 1048576},
		{containerKey{"kube-system", "coredns-xyz", "app"}, 20971520},
	}

	if len(result) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(result))
	}

	for _, tt := range tests {
		got, ok := result[tt.key]
		if !ok {
			t.Errorf("missing key %+v", tt.key)
			continue
		}
		if got != tt.want {
			t.Errorf("key %+v: got %d, want %d", tt.key, got, tt.want)
		}
	}
}

func TestParseContainerMemory_SkipsComments(t *testing.T) {
	metrics := `# HELP container_memory_working_set_bytes help text
# TYPE container_memory_working_set_bytes gauge
container_memory_working_set_bytes{container="app",namespace="ns",pod="p"} 100
`
	result := parseContainerMemory(metrics)
	if len(result) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(result))
	}
}

func TestParseContainerMemory_SkipsEmptyLabels(t *testing.T) {
	metrics := `container_memory_working_set_bytes{container="",namespace="ns",pod="p"} 100
`
	result := parseContainerMemory(metrics)
	if len(result) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(result))
	}
}

func TestExtractLabel(t *testing.T) {
	line := `container_memory_working_set_bytes{container="nginx",namespace="default",pod="web-abc"} 100`
	tests := []struct {
		label, want string
	}{
		{"container", "nginx"},
		{"namespace", "default"},
		{"pod", "web-abc"},
		{"missing", ""},
	}
	for _, tt := range tests {
		got := extractLabel(line, tt.label)
		if got != tt.want {
			t.Errorf("extractLabel(%q) = %q, want %q", tt.label, got, tt.want)
		}
	}
}
