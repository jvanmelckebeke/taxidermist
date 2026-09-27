#!/usr/bin/env python3
"""Print each file's frontmatter as PyYAML's safe_load sees it, one canonical line per file.

main.go prints the same lines from the Go parser; diffing the two is the parity check.

    scripts/pyyaml-parity/canon.py FILE [FILE ...]
    scripts/pyyaml-parity/canon.py --scalars CASES   # one YAML value per line
"""
import datetime
import json
import sys

import yaml


def canon(v):
    if v is None:
        return "null"
    if isinstance(v, bool):
        return f"bool:{v}"
    if isinstance(v, int):
        return f"int:{v}"
    if isinstance(v, float):
        return f"float:{v!r}"
    if isinstance(v, str):
        return "str:" + json.dumps(v, ensure_ascii=False)
    if isinstance(v, datetime.datetime):
        return f"datetime:{v}"
    if isinstance(v, datetime.date):
        return f"date:{v}"
    if isinstance(v, list):
        return "[" + ",".join(canon(x) for x in v) + "]"
    if isinstance(v, dict):
        return "{" + ",".join(canon(k) + "=" + canon(x) for k, x in v.items()) + "}"
    return f"other:{type(v).__name__}"


def block(text):
    """The line-based rule internal/frontmatter uses: `---` line to next `---` line."""
    first, _, rest = text.partition("\n")
    if first.rstrip(" \t\r") != "---":
        return None, False
    lines = []
    while True:
        line, sep, rest = rest.partition("\n")
        if line.rstrip(" \t\r") == "---":
            return "\n".join(lines), True
        if not sep:
            return None, True
        lines.append(line.removesuffix("\r"))


def load(src):
    try:
        return canon(yaml.safe_load(src))
    except Exception:  # ValueError from a bad date is as fatal as a YAMLError
        return "ERROR"


def main():
    args = sys.argv[1:]
    if args[:1] == ["--scalars"]:
        for line in open(args[1], encoding="utf-8").read().splitlines():
            print(f"{line}\t{load('v: ' + line)}")
        return
    for path in args:
        try:
            text = open(path, encoding="utf-8").read()
        except (OSError, UnicodeDecodeError):
            print(f"{path}\tnone")
            continue
        src, opened = block(text)
        if not opened:
            print(f"{path}\tnone")
        elif src is None:
            print(f"{path}\tERROR")
        else:
            print(f"{path}\t{load(src)}")


if __name__ == "__main__":
    main()
