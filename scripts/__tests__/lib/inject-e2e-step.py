#!/usr/bin/env python3
"""Insert a `run:` step into a fixture's e2e-test job, and prove it landed there.

Used by scripts/__tests__/check-runner-portability.test.sh. It exists as a file rather than an
inline heredoc because the suite is itself a heredoc-heavy shell script, and nesting a Python
heredoc inside it terminated the outer one early.

The validation is the point. The first draft of that suite mutated fixtures with
`printf >> test.yml`, which appends to the END of the file -- inside the mac-only `backend-test`
job, which is deliberately exempt from every Linux rule. Four cases therefore reported the guard
as broken when the mutation had simply gone to the wrong job. An unvalidated mutation fakes both
a hole and a guard.
"""
import pathlib
import sys

import yaml

path, step = pathlib.Path(sys.argv[1]), sys.argv[2]
text = path.read_text()

ANCHOR = "      - run: bun run test:e2e\n"
if ANCHOR not in text:
    sys.exit("fixture invalid: the e2e-test anchor line is missing")

path.write_text(text.replace(ANCHOR, ANCHOR + "      - run: " + step + "\n", 1))

job = yaml.safe_load(path.read_text())["jobs"]["e2e-test"]
bodies = "\n".join(str(s.get("run", "")) for s in job["steps"])
if step not in bodies:
    sys.exit("mutation did not land in e2e-test -- the assertion would be vacuous")
