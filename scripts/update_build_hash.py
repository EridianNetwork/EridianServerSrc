# Updates config/server.json's game.build_hash in place, preserving the rest of the file. Called by
# scripts/hooks/post-commit after every commit. see that file contents for how to enable the hook.
import json
import sys
import os

CONFIG_PATH = os.path.join(os.path.dirname(__file__), "..", "config", "server.json")


def main():
    if len(sys.argv) != 2:
        print("usage: update_build_hash.py <short-hash>", file=sys.stderr)
        sys.exit(1)
    short_hash = sys.argv[1]

    with open(CONFIG_PATH, encoding="utf-8") as f:
        cfg = json.load(f)
    cfg.setdefault("game", {})["build_hash"] = short_hash
    with open(CONFIG_PATH, "w", encoding="utf-8") as f:
        json.dump(cfg, f, indent=4)
        f.write("\n")
    print(f"[update_build_hash] game.build_hash = {short_hash}")


if __name__ == "__main__":
    main()
