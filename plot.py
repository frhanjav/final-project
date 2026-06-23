import sys
from pathlib import Path

import matplotlib.dates as mdates
import matplotlib.pyplot as plt
import numpy as np
import pandas as pd

TIER_COLORS = {0: "#c0392b", 1: "#f39c12", 2: "#27ae60"}
BASELINE_LABEL_PREFIXES = ("cold-baseline-", "warm-runtime-", "hot-paused-")


def load_csv(path: Path) -> pd.DataFrame:
    if not path.exists():
        raise SystemExit(f"{path} not found")

    df = pd.read_csv(path)
    if df.empty:
        raise SystemExit(f"{path} is empty")

    if "Timestamp" not in df.columns and len(df.columns) == 5:
        df.columns = [
            "Timestamp",
            "RequestID",
            "TierUsed",
            "LatencyMS",
            "ActiveContainersPoolSize",
        ]
    return df


def plot_avg_latency_by_tier(request_df: pd.DataFrame, output_path: str, title: str) -> None:
    latency = (
        request_df.groupby("TierUsed", as_index=False)["LatencyMS"]
        .mean()
        .sort_values("TierUsed")
    )

    colors = [TIER_COLORS.get(int(tier), "#34495e") for tier in latency["TierUsed"]]
    fig, ax = plt.subplots(figsize=(8, 5))
    ax.bar(latency["TierUsed"].astype(str), latency["LatencyMS"], color=colors)
    ax.set_title(title)
    ax.set_xlabel("Tier")
    ax.set_ylabel("Average Latency (ms)")
    ax.grid(axis="y", alpha=0.25, linestyle="--")
    fig.tight_layout()
    fig.savefig(output_path, dpi=200)
    plt.close(fig)


def plot_pool_size_over_time(request_df: pd.DataFrame) -> None:
    fig, ax = plt.subplots(figsize=(10, 5))
    ax.plot(
        request_df["Timestamp"],
        request_df["ActiveContainersPoolSize"],
        marker="o",
        linewidth=2,
        color="#1f618d",
    )
    ax.set_title("Active Container Pool Size Over Time")
    ax.set_xlabel("Timestamp")
    ax.set_ylabel("Active Containers Pool Size")
    ax.grid(alpha=0.25, linestyle="--")

    if len(request_df) == 1:
        ts = request_df["Timestamp"].iloc[0]
        ax.set_xlim(ts - pd.Timedelta(seconds=1), ts + pd.Timedelta(seconds=1))

    ax.xaxis.set_major_formatter(mdates.DateFormatter("%H:%M:%S"))
    fig.autofmt_xdate(rotation=30)
    fig.tight_layout()
    fig.savefig("pool_size_over_time.png", dpi=200)
    plt.close(fig)


def plot_latency_over_time_by_tier(request_df: pd.DataFrame) -> None:
    fig, ax = plt.subplots(figsize=(10, 5))
    for tier in sorted(request_df["TierUsed"].unique()):
        tier_df = request_df[request_df["TierUsed"] == tier]
        ax.scatter(
            tier_df["Timestamp"],
            tier_df["LatencyMS"],
            label=f"Tier {tier}",
            color=TIER_COLORS.get(int(tier), "#34495e"),
            s=36,
            alpha=0.8,
        )

    ax.set_title("Invocation Latency Over Time by Tier")
    ax.set_xlabel("Timestamp")
    ax.set_ylabel("Latency (ms)")
    ax.grid(alpha=0.25, linestyle="--")
    ax.legend()
    ax.xaxis.set_major_formatter(mdates.DateFormatter("%H:%M:%S"))
    fig.autofmt_xdate(rotation=30)
    fig.tight_layout()
    fig.savefig("latency_over_time_by_tier.png", dpi=200)
    plt.close(fig)


def plot_loadtest_latency_summary(load_df: pd.DataFrame) -> None:
    labels = load_df["RunLabel"].astype(str).tolist()
    x = np.arange(len(labels))
    width = 0.25

    fig, ax = plt.subplots(figsize=(11, 5))
    ax.bar(x - width, load_df["AvgLatencyMS"], width=width, label="Avg", color="#5dade2")
    ax.bar(x, load_df["P95LatencyMS"], width=width, label="P95", color="#f5b041")
    ax.bar(x + width, load_df["P99LatencyMS"], width=width, label="P99", color="#cb4335")

    ax.set_title("Load Test Latency Summary by Scenario")
    ax.set_xlabel("Scenario")
    ax.set_ylabel("Latency (ms)")
    ax.set_xticks(x)
    ax.set_xticklabels(labels, rotation=25, ha="right")
    ax.legend()
    ax.grid(axis="y", alpha=0.25, linestyle="--")
    fig.tight_layout()
    fig.savefig("loadtest_latency_summary.png", dpi=200)
    plt.close(fig)


def main() -> None:
    metrics_path = Path(sys.argv[1]) if len(sys.argv) > 1 else Path("metrics.csv")
    summary_path = Path(sys.argv[2]) if len(sys.argv) > 2 else Path("loadtest_summary.csv")

    df = load_csv(metrics_path)
    df["Timestamp"] = pd.to_datetime(df["Timestamp"])
    request_df = df[df["TierUsed"].isin([0, 1, 2])].copy()
    if request_df.empty:
        raise SystemExit("no tiered invocation rows found in metrics data")

    baseline_df = request_df[
        request_df["RequestID"].astype(str).str.startswith(BASELINE_LABEL_PREFIXES)
    ].copy()
    if baseline_df.empty:
        baseline_df = request_df

    plot_avg_latency_by_tier(
        baseline_df,
        "avg_latency_by_tier.png",
        "Average Invocation Latency by Tier (Controlled Baselines)",
    )
    plot_avg_latency_by_tier(
        request_df,
        "avg_latency_by_tier_all_requests.png",
        "Average Invocation Latency by Tier (All Requests)",
    )
    plot_pool_size_over_time(request_df)
    plot_latency_over_time_by_tier(request_df)

    outputs = [
        "avg_latency_by_tier.png",
        "avg_latency_by_tier_all_requests.png",
        "pool_size_over_time.png",
        "latency_over_time_by_tier.png",
    ]

    if summary_path.exists():
        load_df = pd.read_csv(summary_path)
        if not load_df.empty:
            plot_loadtest_latency_summary(load_df)
            outputs.append("loadtest_latency_summary.png")

    print("generated " + ", ".join(outputs))


if __name__ == "__main__":
    main()
