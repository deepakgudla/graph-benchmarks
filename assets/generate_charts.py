"""
Regenerates the chart images embedded in README.md from the raw numbers in
results/RESULTS.md. Not part of the benchmark harness itself - this is a
one-off reporting script, kept here (not in cmd/) so it's clear it's for
documentation, not for re-running the benchmark.

Usage: python3 assets/generate_charts.py
Requires: pip install matplotlib --break-system-packages
"""
import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt
import numpy as np

plt.rcParams.update({
    "font.size": 11,
    "axes.spines.top": False,
    "axes.spines.right": False,
    "axes.grid": True,
    "grid.alpha": 0.25,
    "figure.facecolor": "white",
    "axes.facecolor": "white",
})

PLATFORMS = ["CognoDB", "Neo4j Aura", "Memgraph", "ArangoDB"]
COLORS = ["#4C72B0", "#55A868", "#C44E52", "#8172B2"]

# ---------------------------------------------------------------------------
# Chart 1: Load throughput (log scale - Memgraph's rel/s is ~300x smaller
# than the fastest platform, a linear axis would make it invisible)
# ---------------------------------------------------------------------------
node_tput = [1136.5, 8808.6, 53.6, 4426.4]
rel_tput = [1895.6, 8590.5, 29.6, 8918.0]

fig, ax = plt.subplots(figsize=(8, 4.5))
x = np.arange(len(PLATFORMS))
width = 0.35
ax.bar(x - width/2, node_tput, width, label="Nodes/sec", color="#4C72B0")
ax.bar(x + width/2, rel_tput, width, label="Relationships/sec", color="#C44E52")
ax.set_yscale("log")
ax.set_ylabel("Throughput (items/sec, log scale)")
ax.set_title("Data load throughput by platform")
ax.set_xticks(x)
ax.set_xticklabels(PLATFORMS)
ax.legend()
for i, v in enumerate(node_tput):
    ax.text(i - width/2, v * 1.15, f"{v:,.0f}", ha="center", fontsize=8.5)
for i, v in enumerate(rel_tput):
    ax.text(i + width/2, v * 1.15, f"{v:,.0f}", ha="center", fontsize=8.5)
fig.tight_layout()
fig.savefig("load_throughput.png", dpi=150)
plt.close(fig)

# ---------------------------------------------------------------------------
# Chart 2: Traversal p50 latency, 1/2/3-hop
# ---------------------------------------------------------------------------
hop1 = [303.45, 122.42, 184.28, 26.03]
hop2 = [303.65, 118.99, 184.17, 26.08]
hop3 = [306.47, 121.96, 185.20, 26.01]

fig, ax = plt.subplots(figsize=(8, 4.5))
x = np.arange(len(PLATFORMS))
width = 0.25
ax.bar(x - width, hop1, width, label="1-hop", color="#4C72B0")
ax.bar(x, hop2, width, label="2-hop", color="#55A868")
ax.bar(x + width, hop3, width, label="3-hop", color="#C44E52")
ax.set_ylabel("p50 latency (ms)")
ax.set_title("Traversal latency by hop depth (p50)")
ax.set_xticks(x)
ax.set_xticklabels(PLATFORMS)
ax.legend()
fig.tight_layout()
fig.savefig("traversal_latency.png", dpi=150)
plt.close(fig)

# ---------------------------------------------------------------------------
# Chart 3: Point + indexed lookup, p50/p95 (error bars = p95)
# ---------------------------------------------------------------------------
point_p50 = [305.26, 125.56, 182.87, 26.09]
point_p95 = [314.36, 140.02, 195.33, 48.71]
idx_p50 = [349.25, 126.09, 155.73, 25.98]
idx_p95 = [661.38, 143.16, 166.24, 32.93]

fig, ax = plt.subplots(figsize=(8, 4.5))
x = np.arange(len(PLATFORMS))
width = 0.35
point_err = [[p50 - 0 for p50 in [0]*4], [p95 - p50 for p50, p95 in zip(point_p50, point_p95)]]
idx_err = [[0]*4, [p95 - p50 for p50, p95 in zip(idx_p50, idx_p95)]]
ax.bar(x - width/2, point_p50, width, yerr=point_err, capsize=4, label="Point lookup (p50, err bar to p95)", color="#4C72B0")
ax.bar(x + width/2, idx_p50, width, yerr=idx_err, capsize=4, label="Indexed lookup (p50, err bar to p95)", color="#8172B2")
ax.set_ylabel("Latency (ms)")
ax.set_title("Point vs. indexed lookup latency (p50, whisker to p95)")
ax.set_xticks(x)
ax.set_xticklabels(PLATFORMS)
ax.legend(fontsize=9)
fig.tight_layout()
fig.savefig("lookup_latency.png", dpi=150)
plt.close(fig)

# ---------------------------------------------------------------------------
# Chart 4: Mixed workload QPS by concurrency (line chart)
# NOTE: ArangoDB's line is annotated as unreliable - see README caveats,
# its error counts (116/1021/2167) mean most of its "successful" ops were
# writes silently targeting nonexistent keys succeeding trivially, while
# genuine writes mostly failed. Shown for completeness, not as a clean win.
# ---------------------------------------------------------------------------
concurrency = [1, 10, 40]
qps = {
    "CognoDB": [3.4, 29.6, 92.7],
    "Neo4j Aura": [8.2, 75.7, 288.3],
    "Memgraph": [5.2, 35.2, 37.1],
    "ArangoDB*": [34.5, 297.9, 635.5],
}
fig, ax = plt.subplots(figsize=(8, 4.5))
for (label, vals), color in zip(qps.items(), COLORS):
    style = "--" if "*" in label else "-"
    ax.plot(concurrency, vals, style, marker="o", label=label, color=color, linewidth=2)
ax.set_xscale("log")
ax.set_xticks(concurrency)
ax.set_xticklabels([str(c) for c in concurrency])
ax.set_xlabel("Concurrent clients")
ax.set_ylabel("Queries / second")
ax.set_title("Mixed read/write throughput vs. concurrency")
ax.legend(fontsize=9)
fig.text(0.5, -0.02, "* ArangoDB's numbers here are affected by the mixed-workload ID bug - see README Caveats.",
          ha="center", fontsize=8, style="italic", color="#555555")
fig.tight_layout()
fig.savefig("mixed_workload_qps.png", dpi=150, bbox_inches="tight")
plt.close(fig)

print("Wrote 4 charts to assets/")
