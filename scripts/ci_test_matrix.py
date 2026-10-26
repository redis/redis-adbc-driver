"""Select Redis CI coverage from the entire PR diff; other events run everything."""

import argparse
import fnmatch
import json
import subprocess


SENSITIVE_PATHS = (
    ".github/workflows/test.yml",
    "scripts/ci_test_matrix.py",
    "scripts/test_ci_test_matrix.py",
    "go/database*",
    "go/connection*",
    "go/driver*",
    "go/store*",
    "go/pkg/**",
    "go/*cluster*",
    "go/*partition*",
    "go/*tls*",
    "go/*timeout*",
    "go/*acl*",
    "go/search_caps*",
    "go/validation/tests/test_connection.py",
    "go/validation/tests/test_partitions*.py",
    "go/compose.yaml",
    "go/software-*.sh",
    "go/flex-setup.sh",
    "scripts/test_software_qpf.py",
    "go/Makefile",
    "go/go.mod",
    "go/go.sum",
    "scripts/*partition*",
    "scripts/*cluster*",
)


def select_matrix(event, paths):
    full = event != "pull_request" or any(
        not path.endswith(".md") and fnmatch.fnmatchcase(path, pattern)
        for path in paths
        for pattern in SENSITIVE_PATHS
    )
    environments = (
        ["software-single", "software-cluster", "oss-single", "oss-cluster"]
        if full else ["software-single", "oss-cluster"]
    )
    # Keep Python integrations and the standalone TLS fixture on exactly one
    # environment per architecture, including when oss-single is omitted.
    integrations = "oss-single" if full else "software-single"
    return {"include": [
        {"arch": arch, "runner": runner, "environment": environment,
         "integrations": environment == integrations}
        for arch, runner in (
            [("amd64", "ubuntu-24.04"), ("arm64", "ubuntu-24.04-arm")]
            if full else [("amd64", "ubuntu-24.04")]
        )
        for environment in environments
    ]}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--event", required=True)
    parser.add_argument("--base")
    parser.add_argument("--head")
    args = parser.parse_args()
    paths = []
    if args.event == "pull_request":
        if not args.base or not args.head:
            parser.error("PR selection requires both --base and --head")
        # --no-renames includes both old and new paths, so moving a sensitive
        # file cannot accidentally reduce coverage. Fail closed on diff errors.
        output = subprocess.check_output([
            "git", "diff", "--name-only", "--no-renames", "-z",
            args.base, args.head, "--",
        ])
        paths = output.decode().rstrip("\0").split("\0")
    print(json.dumps(select_matrix(args.event, paths), separators=(",", ":")))


if __name__ == "__main__":
    main()
