Kubernetes-Eager-OOM-Killer
===========================

Kubernetes containers can have memory limits set. These limits are only
enforced when a node has memory pressure, so it is easy to have containers
running well over their limits for a long time and only discover it when the
system gets a load spike.

This tool watches a cluster and preemptively kills any pod that has containers
over their limit, whether the node has pressure or not. It generates similar
events to the real OOM killer.
