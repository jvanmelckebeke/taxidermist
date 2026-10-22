"""Tests against a taxidermist built from this checkout, run on a copy of example/.

$TAXIDERMIST_BIN, when set, skips the build and tests that binary instead.
"""

import os
import shutil
import stat
import subprocess
from pathlib import Path

import pytest

import taxidermist
from taxidermist import BinaryNotFound, BinaryTooOld, Finding, TaxidermistError

REPO = Path(__file__).resolve().parents[2]

BAD_NOTE = "---\ntype: note\ntitle: t\ntopic: nope\ndate: 2026-01-01\n---\n"


@pytest.fixture(scope="session")
def binary(tmp_path_factory):
    if os.environ.get(taxidermist.ENV_VAR):
        return os.environ[taxidermist.ENV_VAR]
    if shutil.which("go") is None:
        pytest.skip("go is not installed and $TAXIDERMIST_BIN is not set")
    out = tmp_path_factory.mktemp("bin") / "taxidermist"
    subprocess.run(["go", "build", "-o", str(out), "./cmd/taxidermist"], cwd=REPO, check=True)
    return str(out)


@pytest.fixture
def kb(tmp_path, monkeypatch, binary):
    monkeypatch.setenv(taxidermist.ENV_VAR, binary)
    shutil.copytree(REPO / "example", tmp_path, dirs_exist_ok=True)
    monkeypatch.chdir(tmp_path)
    return tmp_path


def fake_binary(dir: Path, version: str) -> str:
    p = dir / "taxidermist"
    p.write_text(f"#!/bin/sh\necho 'taxidermist {version}'\n")
    p.chmod(p.stat().st_mode | stat.S_IEXEC)
    return str(p)


def test_validate_example_is_clean(kb):
    r = taxidermist.validate()
    assert r.ok
    assert r.governed > 0
    assert r.faults == [] and r.warnings == []


def test_validate_files(kb):
    (kb / "notes" / "bad.md").write_text(BAD_NOTE)
    r = taxidermist.validate("notes/bad.md", kb / "elsewhere" / "x.md")
    assert not r.ok
    assert r.governed == 1
    f = r.faults[0]
    assert (f.file, f.kind, f.field, f.value) == ("notes/bad.md", "value", "topic", "nope")
    assert f.why


def test_validate_content(kb):
    r = taxidermist.validate_content("---\ntype: note\ntitle: t\n---\n", "notes/new.md")
    assert not (kb / "notes" / "new.md").exists()
    assert {(f.kind, f.field) for f in r.faults} == {("missing", "topic"), ("missing", "date")}
    assert all(isinstance(f, Finding) and f.value is None for f in r.faults)

    r = taxidermist.validate_content(BAD_NOTE.replace("nope", "garden").encode(), "notes/new.md")
    assert r.ok


def test_content_outside_the_roots(kb):
    with pytest.raises(TaxidermistError, match="elsewhere/x.md"):
        taxidermist.validate_content(BAD_NOTE, "elsewhere/x.md")


def test_broken_taxonomy(kb):
    (kb / "taxonomy" / "types" / "note.yaml").write_text("required: [\n")
    with pytest.raises(TaxidermistError, match="note.yaml"):
        taxidermist.validate()


def test_warnings_do_not_fail(kb):
    (kb / "projects" / "infra" / "w.md").write_text("---\ntype: Project Plan\ntitle: w\ntopic: hiring\n---\n")
    r = taxidermist.validate("projects/infra/w.md")
    assert r.ok
    assert [(w.kind, w.field, w.value) for w in r.warnings] == [("mismatch", "topic", "hiring")]


def test_binary_missing(monkeypatch, tmp_path):
    monkeypatch.delenv(taxidermist.ENV_VAR, raising=False)
    monkeypatch.setenv("PATH", str(tmp_path))
    with pytest.raises(BinaryNotFound, match="not on PATH"):
        taxidermist.find_binary()
    monkeypatch.setenv(taxidermist.ENV_VAR, str(tmp_path / "nope"))
    with pytest.raises(BinaryNotFound, match="TAXIDERMIST_BIN"):
        taxidermist.find_binary()


def test_binary_on_path(monkeypatch, tmp_path):
    monkeypatch.delenv(taxidermist.ENV_VAR, raising=False)
    monkeypatch.setenv("PATH", str(tmp_path))
    path = fake_binary(tmp_path, "9.0.0")
    assert taxidermist.find_binary() == path


def test_binary_too_old(monkeypatch, tmp_path):
    monkeypatch.setenv(taxidermist.ENV_VAR, fake_binary(tmp_path, "0.5.1"))
    with pytest.raises(BinaryTooOld, match="0.5.1"):
        taxidermist.validate()


def test_not_taxidermist(monkeypatch, tmp_path):
    monkeypatch.setenv(taxidermist.ENV_VAR, fake_binary(tmp_path, "who knows"))
    with pytest.raises(TaxidermistError, match="does not look like taxidermist"):
        taxidermist.find_binary()


def test_unknown_json_keys_are_ignored(monkeypatch, tmp_path):
    p = tmp_path / "taxidermist"
    p.write_text(
        "#!/bin/sh\n"
        'if [ "$1" = version ]; then echo "taxidermist dev"; exit 0; fi\n'
        'echo \'{"governed": 1, "new": 1, "faults": [{"file": "a.md", "kind": "missing", '
        '"field": "topic", "value": null, "why": "w", "line": 3}], "warnings": [], "ungoverned": {}}\'\n'
        "exit 1\n"
    )
    p.chmod(p.stat().st_mode | stat.S_IEXEC)
    monkeypatch.setenv(taxidermist.ENV_VAR, str(p))
    r = taxidermist.validate()
    assert r.faults == [Finding("a.md", "missing", "topic", None, "w")]
