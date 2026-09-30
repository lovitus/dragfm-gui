#!/usr/bin/env python3
"""Bind successfully completed hosted stages and exact files to their checkout."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('directory', type=Path)
    parser.add_argument('--stage', action='append', required=True)
    parser.add_argument('--file', action='append', required=True)
    args = parser.parse_args()
    if os.environ.get('GITHUB_ACTIONS') != 'true':
        raise RuntimeError('Evidence receipts require a disposable hosted workflow')
    commit = subprocess.check_output(['git', 'rev-parse', 'HEAD'], text=True).strip()
    if commit != os.environ['GITHUB_SHA']:
        raise RuntimeError('Evidence checkout differs from the event commit')
    head = os.environ.get('CANDIDATE_SHA', commit)
    if not re.fullmatch(r'[0-9a-f]{40}', head):
        raise RuntimeError('Invalid candidate source identifier')
    directory = args.directory.resolve(strict=True)
    files = {}
    for name in args.file:
        path = Path(name)
        if path.is_absolute() or '..' in path.parts:
            raise RuntimeError('Unsafe evidence path')
        resolved = (directory / path).resolve(strict=True)
        if not resolved.is_relative_to(directory) or not resolved.is_file():
            raise RuntimeError('Evidence is not a contained regular file')
        with resolved.open('rb') as source:
            files[name] = hashlib.file_digest(source, 'sha256').hexdigest()
    receipt = dict(schema=1, success=True, commit=commit, event_commit=os.environ['GITHUB_SHA'],
                   tree=subprocess.check_output(['git', 'rev-parse', 'HEAD^{tree}'], text=True).strip(),
                   candidate_head=head, run=os.environ['GITHUB_RUN_ID'],
                   attempt=os.environ['GITHUB_RUN_ATTEMPT'], stages=sorted(set(args.stage)), files=files)
    with (directory / 'ARTIFACT_RECEIPT.json').open('x', encoding='utf-8') as output:
        json.dump(receipt, output, indent=2, sort_keys=True)
        output.write('\n')


if __name__ == '__main__':
    main()
