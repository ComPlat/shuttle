#!/usr/bin/env python3
"""Reproduce ShuttleBuilder's template pass over every tracked file.

ShuttleBuilder reads every file of the repo as text and renders it through a
Django Template before compiling. One file that fails here breaks the build for
every lab. Only needs Django (pip install django). Python 3.8+.
"""
import os
import subprocess
import sys

import django
from django.conf import settings

settings.configure(
    TEMPLATES=[{"BACKEND": "django.template.backends.django.DjangoTemplates"}]
)
django.setup()

from django.template import Context, Template  # noqa: E402

OPEN_VAR = "{" + "{"
CLOSE_VAR = "}" + "}"
WARN_EXEMPT = ("cmd_manager.go",)


def main():
    root = subprocess.check_output(
        ["git", "rev-parse", "--show-toplevel"],
        cwd=os.path.dirname(os.path.abspath(__file__)),
        universal_newlines=True,
    ).strip()
    out = subprocess.check_output(["git", "ls-files", "-z"], cwd=root)
    files = [f for f in out.decode("utf-8").split("\0") if f]

    failures = []
    warnings = []
    checked = 0
    for rel in files:
        path = os.path.join(root, rel)
        if not os.path.isfile(path):  # deleted in working tree / submodule
            continue
        checked += 1
        try:
            with open(path, "rb") as fh:
                text = fh.read().decode("utf-8")
        except UnicodeDecodeError as e:
            failures.append((rel, "UnicodeDecodeError: %s" % e))
            continue
        try:
            Template(text).render(Context({}))
        except Exception as e:  # any exception breaks ShuttleBuilder
            failures.append((rel, "%s: %s" % (type(e).__name__, e)))
            continue
        base = os.path.basename(rel)
        if (
            OPEN_VAR in text
            and CLOSE_VAR in text
            and base not in WARN_EXEMPT
            and not base.lower().endswith(".md")
        ):
            warnings.append(rel)

    for rel in warnings:
        print("WARNING: %s contains a double-brace expression; "
              "ShuttleBuilder will blank or alter it" % rel)
    if failures:
        print("FAILED: %d of %d files do not survive the template pass:"
              % (len(failures), checked))
        for rel, msg in failures:
            print("  %s: %s" % (rel, msg))
        return 1
    print("OK: %d tracked files render cleanly (%d warning(s))"
          % (checked, len(warnings)))
    return 0


if __name__ == "__main__":
    sys.exit(main())
