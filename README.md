Kubernetes-Eager-OOM-Killer
===========================

Kubernetes containers can have memory limits set. These limits are only
enforced when a node has memory pressure, so it is easy to have containers
running well over their limits for a long time and only discover it when the
system gets a load spike.

This tool watches a cluster and preemptively kills any pod that has containers
over their limit, whether the node has pressure or not. It generates similar
events to the real OOM killer.

Modes
-----

### Kubelet mode (default)

Runs as a single **Deployment**. Polls every node's kubelet `/metrics/resource`
endpoint via the API server proxy to read `container_memory_working_set_bytes`,
then compares against the pod spec's memory limit.

```bash
helm install eager-oom-killer ./helm
# or explicitly:
helm install eager-oom-killer ./helm --set mode=kubelet
```

### Cgroup mode

Runs as a **DaemonSet** on every node. Reads cgroup v2 files
(`memory.current` and `memory.max`) directly from the host filesystem.
This avoids API server proxying and uses the same values the kernel uses for
OOM decisions.

Requires:
- cgroup v2 (unified hierarchy) on the host
- containerd or CRI-O container runtime

```bash
helm install eager-oom-killer ./helm --set mode=cgroup
```

Configuration
-------------

| Value        | Default          | Description                                              |
|--------------|------------------|----------------------------------------------------------|
| `mode`       | `kubelet`        | `kubelet` (Deployment) or `cgroup` (DaemonSet)           |
| `interval`   | `1m`             | Polling interval (e.g. `30s`, `2m`)                      |
| `cgroupRoot` | `/sys/fs/cgroup` | Host cgroup v2 mount path (cgroup mode only)             |
| `image.repository` | `eager-oom-killer` | Container image repository                        |
| `image.tag`  | `latest`         | Container image tag                                      |
