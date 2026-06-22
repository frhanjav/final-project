import sys
import time


def main() -> None:
    sleep_ms = 1000
    if len(sys.argv) > 1:
        sleep_ms = int(sys.argv[1])

    time.sleep(sleep_ms / 1000.0)


if __name__ == "__main__":
    main()
