"""Audit the full merged-main rerun after PRs #415–418."""
import json
from pathlib import Path
import sys

from merged_perf_audit import summarize

COMMIT = '5f759300c12f5191a36d2f2b5a29555ef14ef486'
VERSION = 'v0.1.0-dev.merged-perf-2'


def main():
    directory = Path(sys.argv[1]) if len(sys.argv) > 1 else Path(__file__).parent / 'merged-main-2026-11-13-rerun'
    actual = summarize(json.loads((directory / 'measurements.json').read_text()), commit=COMMIT, version=VERSION)
    assert actual == json.loads((directory / 'summary.json').read_text())
    print('PASS: all 156 rerun trials; complete validation, coverage, source/version, QPF and arithmetic')


if __name__ == '__main__':
    main()
