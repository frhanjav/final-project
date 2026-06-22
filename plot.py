import sys
from pathlib import Path

import matplotlib.pyplot as plt
import pandas as pd


def main() -> None:
    metrics_path = Path(sys.argv[1]) if len(sys.argv) > 1 else Path("metrics.csv")
    if not metrics_path.exists():
        raise SystemExit(f"{metrics_path} not found")

    df = pd.read_csv(metrics_path)
    if df.empty:
        raise SystemExit("metrics.csv is empty")

    df["Timestamp"] = pd.to_datetime(df["Timestamp"])
    request_df = df[df["TierUsed"].isin([0, 1, 2])].copy()

    latency = request_df.groupby("TierUsed", as_index=False)["LatencyMS"].mean()
    latency = latency.sort_values("TierUsed")

    plt.figure(figsize=(8, 5))
    plt.bar(latency["TierUsed"].astype(str), latency["LatencyMS"], color=["#c0392b", "#f39c12", "#27ae60"])
    plt.title("Average Invocation Latency by Tier")
    plt.xlabel("Tier")
    plt.ylabel("Average Latency (ms)")
    plt.tight_layout()
    plt.savefig("avg_latency_by_tier.png", dpi=200)
    plt.close()

    plt.figure(figsize=(10, 5))
    plt.plot(request_df["Timestamp"], request_df["ActiveContainersPoolSize"], marker="o", linewidth=2, color="#1f618d")
    plt.title("Active Container Pool Size Over Time")
    plt.xlabel("Timestamp")
    plt.ylabel("Active Containers Pool Size")
    plt.xticks(rotation=30, ha="right")
    plt.tight_layout()
    plt.savefig("pool_size_over_time.png", dpi=200)
    plt.close()

    print("generated avg_latency_by_tier.png and pool_size_over_time.png")


if __name__ == "__main__":
    main()
