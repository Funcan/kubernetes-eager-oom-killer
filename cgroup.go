package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// cgroupEntry represents a container's memory state read from cgroup v2 files.
type cgroupEntry struct {
	podUID      string
	containerID string
	memCurrent  int64
	memMax      int64 // -1 means "max" (no limit)
}

// scanCgroups walks the kubepods cgroup tree and returns memory data for each container.
func scanCgroups(cgroupRoot string) ([]cgroupEntry, error) {
	kubepods := filepath.Join(cgroupRoot, "kubepods.slice")
	if _, err := os.Stat(kubepods); err != nil {
		return nil, fmt.Errorf("kubepods cgroup not found at %s: %w", kubepods, err)
	}

	var entries []cgroupEntry

	// Guaranteed QoS pods live directly under kubepods.slice
	guaranteed, err := scanQoSDir(kubepods)
	if err != nil {
		return nil, err
	}
	entries = append(entries, guaranteed...)

	// Burstable and besteffort pods live under their own sub-slices
	for _, qos := range []string{"burstable", "besteffort"} {
		qosDir := filepath.Join(kubepods, fmt.Sprintf("kubepods-%s.slice", qos))
		if _, err := os.Stat(qosDir); err != nil {
			continue
		}
		qosEntries, err := scanQoSDir(qosDir)
		if err != nil {
			return nil, err
		}
		entries = append(entries, qosEntries...)
	}

	return entries, nil
}

// scanQoSDir scans a directory for pod subdirectories and their container cgroups.
func scanQoSDir(dir string) ([]cgroupEntry, error) {
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading directory %s: %w", dir, err)
	}

	var entries []cgroupEntry
	for _, de := range dirEntries {
		if !de.IsDir() {
			continue
		}
		podUID := parsePodUID(de.Name())
		if podUID == "" {
			continue
		}
		podDir := filepath.Join(dir, de.Name())
		containerEntries, err := scanPodDir(podDir, podUID)
		if err != nil {
			return nil, err
		}
		entries = append(entries, containerEntries...)
	}
	return entries, nil
}

// scanPodDir scans a pod cgroup directory for container scope directories.
func scanPodDir(podDir, podUID string) ([]cgroupEntry, error) {
	dirEntries, err := os.ReadDir(podDir)
	if err != nil {
		return nil, fmt.Errorf("reading pod dir %s: %w", podDir, err)
	}

	var entries []cgroupEntry
	for _, de := range dirEntries {
		if !de.IsDir() {
			continue
		}
		containerID := parseContainerID(de.Name())
		if containerID == "" {
			continue
		}
		containerDir := filepath.Join(podDir, de.Name())
		entry, err := readContainerMemory(containerDir, podUID, containerID)
		if err != nil {
			continue // skip containers we can't read
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// readContainerMemory reads memory.current and memory.max from a container's cgroup dir.
func readContainerMemory(dir, podUID, containerID string) (cgroupEntry, error) {
	current, err := readCgroupInt64(filepath.Join(dir, "memory.current"))
	if err != nil {
		return cgroupEntry{}, fmt.Errorf("reading memory.current: %w", err)
	}

	max, err := readCgroupMax(filepath.Join(dir, "memory.max"))
	if err != nil {
		return cgroupEntry{}, fmt.Errorf("reading memory.max: %w", err)
	}

	return cgroupEntry{
		podUID:      podUID,
		containerID: containerID,
		memCurrent:  current,
		memMax:      max,
	}, nil
}

// readCgroupInt64 reads a single int64 value from a cgroup file.
func readCgroupInt64(path string) (int64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
}

// readCgroupMax reads a cgroup memory.max file. Returns -1 for "max" (no limit).
func readCgroupMax(path string) (int64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	s := strings.TrimSpace(string(data))
	if s == "max" {
		return -1, nil
	}
	return strconv.ParseInt(s, 10, 64)
}

// parsePodUID extracts a pod UID from a cgroup directory name.
// Examples:
//
//	"kubepods-burstable-pod1a2b3c4d_5e6f_7a8b_9c0d_1e2f3a4b5c6d.slice" → "1a2b3c4d-5e6f-7a8b-9c0d-1e2f3a4b5c6d"
//	"kubepods-pod1a2b3c4d_5e6f_7a8b_9c0d_1e2f3a4b5c6d.slice"           → "1a2b3c4d-5e6f-7a8b-9c0d-1e2f3a4b5c6d"
func parsePodUID(name string) string {
	// Strip .slice suffix
	name = strings.TrimSuffix(name, ".slice")

	// Find "pod" prefix within the name
	idx := strings.LastIndex(name, "-pod")
	if idx == -1 {
		return ""
	}
	uid := name[idx+4:] // skip "-pod"
	if uid == "" {
		return ""
	}

	// cgroup v2 uses underscores where UUIDs use hyphens
	return strings.ReplaceAll(uid, "_", "-")
}

// parseContainerID extracts a container ID from a cgroup scope directory name.
// Supports containerd ("cri-containerd-<id>.scope") and CRI-O ("crio-<id>.scope").
func parseContainerID(name string) string {
	name = strings.TrimSuffix(name, ".scope")

	if strings.HasPrefix(name, "cri-containerd-") {
		return strings.TrimPrefix(name, "cri-containerd-")
	}
	if strings.HasPrefix(name, "crio-") {
		return strings.TrimPrefix(name, "crio-")
	}
	return ""
}
