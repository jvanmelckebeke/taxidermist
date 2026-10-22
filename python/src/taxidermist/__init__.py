"""Check markdown frontmatter against a taxonomy directory.

This package runs the taxidermist binary with ``check --json`` and returns its
output as dataclasses. It finds the binary in ``$TAXIDERMIST_BIN``, then on PATH.
"""

from __future__ import annotations

import functools
import json
import os
import re
import shutil
import subprocess
from dataclasses import dataclass, field

__all__ = [
    "ENV_VAR",
    "MIN_VERSION",
    "BinaryNotFound",
    "BinaryTooOld",
    "Finding",
    "Result",
    "TaxidermistError",
    "find_binary",
    "validate",
    "validate_content",
]

# The first release with check --stdin-filename.
MIN_VERSION = (0, 6, 0)

ENV_VAR = "TAXIDERMIST_BIN"

class TaxidermistError(Exception):
    """The binary could not check: a broken taxonomy, a bad path, a usage error."""


class BinaryNotFound(TaxidermistError):
    """No taxidermist binary in $TAXIDERMIST_BIN or on PATH."""


class BinaryTooOld(TaxidermistError):
    """The binary predates MIN_VERSION."""


@dataclass(frozen=True)
class Finding:
    """One fault or warning. ``kind`` is the rule, ``why`` the message."""

    file: str
    kind: str
    field: str
    value: str | None
    why: str


@dataclass(frozen=True)
class Result:
    governed: int
    faults: list[Finding] = field(default_factory=list)
    warnings: list[Finding] = field(default_factory=list)
    ungoverned: dict[str, list[str]] = field(default_factory=dict)

    @property
    def ok(self) -> bool:
        """True when there are no faults. Warnings never fail a check."""
        return not self.faults


def find_binary() -> str:
    """Return the taxidermist binary to run, after checking its version."""
    env = os.environ.get(ENV_VAR)
    if env:
        path = shutil.which(env)
        if path is None:
            raise BinaryNotFound(f"${ENV_VAR} is {env!r}, which is not an executable")
    else:
        path = shutil.which("taxidermist")
        if path is None:
            raise BinaryNotFound(
                "taxidermist is not on PATH. Install a release from "
                "https://github.com/jvanmelckebeke/taxidermist/releases, or "
                f"set ${ENV_VAR} to the binary."
            )
    _check_version(path)
    return path


@functools.lru_cache(maxsize=None)
def _check_version(path: str) -> None:
    out = subprocess.run([path, "version"], capture_output=True, text=True).stdout.strip()
    if out == "taxidermist dev":
        return
    m = re.fullmatch(r"taxidermist v?(\d+)\.(\d+)\.(\d+)\S*", out)
    if m is None:
        raise TaxidermistError(f"{path} does not look like taxidermist: `version` printed {out!r}")
    found = tuple(int(g) for g in m.groups())
    if found < MIN_VERSION:
        want = ".".join(map(str, MIN_VERSION))
        have = ".".join(map(str, found))
        raise BinaryTooOld(f"{path} is taxidermist {have}; this package needs {want} or later")


def validate(*paths: str | os.PathLike[str], taxonomy: str | os.PathLike[str] = "taxonomy") -> Result:
    """Check markdown files on disk, as ``taxidermist check FILE...`` does.

    With no paths it checks every governed root. A path that is not markdown,
    does not exist, or sits outside the roots in scope.yaml is skipped.
    """
    return _run(["--", *(os.fspath(p) for p in paths)], taxonomy, None)


def validate_content(
    content: str | bytes, path: str | os.PathLike[str], *, taxonomy: str | os.PathLike[str] = "taxonomy"
) -> Result:
    """Check ``content`` as if it were the markdown file at ``path``.

    The file need not exist, but ``path`` must sit under a governed root: it
    decides the folder rules, the exemptions and the slug the page must not share.
    """
    data = content.encode() if isinstance(content, str) else content
    return _run(["--stdin-filename", os.fspath(path)], taxonomy, data)


def _run(args: list[str], taxonomy: str | os.PathLike[str], stdin: bytes | None) -> Result:
    cmd = [find_binary(), "check", "--json", "--taxonomy", os.fspath(taxonomy), *args]
    proc = subprocess.run(cmd, input=stdin, capture_output=True)
    if proc.returncode not in (0, 1):
        raise TaxidermistError(proc.stderr.decode(errors="replace").strip() or f"exit {proc.returncode}")
    raw = json.loads(proc.stdout)
    return Result(
        governed=raw["governed"],
        faults=[_finding(f) for f in raw["faults"]],
        warnings=[_finding(f) for f in raw["warnings"]],
        ungoverned=raw["ungoverned"],
    )


def _finding(raw: dict) -> Finding:
    # Read the known keys only, so a key a newer binary adds does not break this.
    return Finding(file=raw["file"], kind=raw["kind"], field=raw["field"], value=raw["value"], why=raw["why"])
