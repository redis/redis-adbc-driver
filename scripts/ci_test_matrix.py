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
    environments = ["software-cluster"]
    integrations = "software-cluster"
    return {"include": [
        {"arch": arch, "runner": runner, "environment": environment,
         "integrations": environment == integrations}
        for arch, runner in (
            [("amd64", "ubuntu-24.04"), ("arm64", "ubuntu-24.04-arm")]
            if full else [("amd64", "ubuntu-24.04")]
        )
        for environment in environments
    ]}


# Changes that can affect the Linux ABI, native loading, dependency resolution,
# fixture behavior or distribution-specific compatibility retain all images.
RELEASE_COMPATIBILITY_PATHS = SENSITIVE_PATHS + (
    ".github/workflows/release.yml",
    ".github/workflows/wheel-client.yml",
    "go/build-linux.sh",
    "go/Dockerfile.linux-builder",
    "go/validation/**",
    "go/foreign.go",
    "go/search_keys*",
    "go/transport*",
    "go/aggregate.go",
    "go/rekey.go",
    "python/pyproject.toml",
    "scripts/build_python_wheel.py",
    "scripts/ci_test_matrix.py",
    "scripts/test_ci_test_matrix.py",
    "scripts/check_wheel_libraries.py",
    "scripts/test_check_wheel_libraries.py",
    "scripts/check_native_install.py",
    "scripts/install*",
    "scripts/test_install_driver.py",
)


def select_release_matrix(event, paths):
    full = event != "pull_request" or any(
        not path.endswith(".md") and fnmatch.fnmatchcase(path, pattern)
        for path in paths
        for pattern in RELEASE_COMPATIBILITY_PATHS
    )
    return {
        "arch": ["amd64", "arm64"],
        # AlmaLinux 8 exercises the oldest supported glibc on both architectures.
        "image": ["ubuntu:20.04", "ubuntu:22.04", "almalinux:8", "almalinux:9",
                  "amazonlinux:2023"] if full else ["almalinux:8"],
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--event", required=True)
    parser.add_argument("--suite", choices=("test", "release"), default="test")
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
    select = select_release_matrix if args.suite == "release" else select_matrix
    print(json.dumps(select(args.event, paths), separators=(",", ":")))


if __name__ == "__main__":
    main()
