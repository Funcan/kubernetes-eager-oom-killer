package main

import (
	"os"
	"path/filepath"
	"testing"
)

// createCgroupTree creates a fake cgroup v2 filesystem for testing.
func createCgroupTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for path, content := range files {
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestParsePodUID(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"kubepods-burstable-pod1a2b3c4d_5e6f_7a8b_9c0d_1e2f3a4b5c6d.slice", "1a2b3c4d-5e6f-7a8b-9c0d-1e2f3a4b5c6d"},
		{"kubepods-pod1a2b3c4d_5e6f_7a8b_9c0d_1e2f3a4b5c6d.slice", "1a2b3c4d-5e6f-7a8b-9c0d-1e2f3a4b5c6d"},
		{"kubepods-besteffort-podabcdef01_2345_6789_abcd_ef0123456789.slice", "abcdef01-2345-6789-abcd-ef0123456789"},
		{"kubepods-burstable.slice", ""},
		{"some-other-dir", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := parsePodUID(tt.name)
		if got != tt.want {
			t.Errorf("parsePodUID(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestParseContainerID(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"cri-containerd-abc123def456.scope", "abc123def456"},
		{"crio-abc123def456.scope", "abc123def456"},
		{"some-other.scope", ""},
		{"init.scope", ""},
	}
	for _, tt := range tests {
		got := parseContainerID(tt.name)
		if got != tt.want {
			t.Errorf("parseContainerID(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestReadCgroupMax(t *testing.T) {
	dir := t.TempDir()

	// Test numeric value
	if err := os.WriteFile(filepath.Join(dir, "numeric"), []byte("104857600\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	val, err := readCgroupMax(filepath.Join(dir, "numeric"))
	if err != nil || val != 104857600 {
		t.Errorf("readCgroupMax(numeric) = %d, %v; want 104857600, nil", val, err)
	}

	// Test "max" (no limit)
	if err := os.WriteFile(filepath.Join(dir, "unlimited"), []byte("max\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	val, err = readCgroupMax(filepath.Join(dir, "unlimited"))
	if err != nil || val != -1 {
		t.Errorf("readCgroupMax(unlimited) = %d, %v; want -1, nil", val, err)
	}
}

func TestScanCgroups(t *testing.T) {
	root := t.TempDir()
	podUID := "1a2b3c4d_5e6f_7a8b_9c0d_1e2f3a4b5c6d"
	containerID := "abc123def456"

	files := map[string]string{
		// Burstable pod with containerd container
		filepath.Join("kubepods.slice", "kubepods-burstable.slice",
			"kubepods-burstable-pod"+podUID+".slice",
			"cri-containerd-"+containerID+".scope",
			"memory.current"): "52428800\n",
		filepath.Join("kubepods.slice", "kubepods-burstable.slice",
			"kubepods-burstable-pod"+podUID+".slice",
			"cri-containerd-"+containerID+".scope",
			"memory.max"): "104857600\n",
	}
	createCgroupTree(t, root, files)

	entries, err := scanCgroups(root)
	if err != nil {
		t.Fatalf("scanCgroups: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	e := entries[0]
	wantUID := "1a2b3c4d-5e6f-7a8b-9c0d-1e2f3a4b5c6d"
	if e.podUID != wantUID {
		t.Errorf("podUID = %q, want %q", e.podUID, wantUID)
	}
	if e.containerID != containerID {
		t.Errorf("containerID = %q, want %q", e.containerID, containerID)
	}
	if e.memCurrent != 52428800 {
		t.Errorf("memCurrent = %d, want 52428800", e.memCurrent)
	}
	if e.memMax != 104857600 {
		t.Errorf("memMax = %d, want 104857600", e.memMax)
	}
}

func TestScanCgroups_GuaranteedPod(t *testing.T) {
	root := t.TempDir()
	podUID := "aaaabbbb_cccc_dddd_eeee_ffff00001111"
	containerID := "guaranteed123"

	files := map[string]string{
		// Guaranteed pods live directly under kubepods.slice
		filepath.Join("kubepods.slice",
			"kubepods-pod"+podUID+".slice",
			"cri-containerd-"+containerID+".scope",
			"memory.current"): "1048576\n",
		filepath.Join("kubepods.slice",
			"kubepods-pod"+podUID+".slice",
			"cri-containerd-"+containerID+".scope",
			"memory.max"): "2097152\n",
	}
	createCgroupTree(t, root, files)

	entries, err := scanCgroups(root)
	if err != nil {
		t.Fatalf("scanCgroups: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].podUID != "aaaabbbb-cccc-dddd-eeee-ffff00001111" {
		t.Errorf("podUID = %q, want guaranteed pod UID", entries[0].podUID)
	}
}

func TestScanCgroups_CriO(t *testing.T) {
	root := t.TempDir()
	podUID := "11112222_3333_4444_5555_666677778888"
	containerID := "crio999"

	files := map[string]string{
		filepath.Join("kubepods.slice", "kubepods-besteffort.slice",
			"kubepods-besteffort-pod"+podUID+".slice",
			"crio-"+containerID+".scope",
			"memory.current"): "500000\n",
		filepath.Join("kubepods.slice", "kubepods-besteffort.slice",
			"kubepods-besteffort-pod"+podUID+".slice",
			"crio-"+containerID+".scope",
			"memory.max"): "max\n",
	}
	createCgroupTree(t, root, files)

	entries, err := scanCgroups(root)
	if err != nil {
		t.Fatalf("scanCgroups: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].containerID != containerID {
		t.Errorf("containerID = %q, want %q", entries[0].containerID, containerID)
	}
	if entries[0].memMax != -1 {
		t.Errorf("memMax = %d, want -1 (unlimited)", entries[0].memMax)
	}
}

func TestScanCgroups_NoKubepods(t *testing.T) {
	root := t.TempDir()
	_, err := scanCgroups(root)
	if err == nil {
		t.Error("expected error when kubepods.slice doesn't exist")
	}
}
