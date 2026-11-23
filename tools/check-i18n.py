#!/usr/bin/env python3
"""Checks that the user-facing English keys of the Go code are known to the interface.

Usage: python3 tools/check-i18n.py [repo_root]

Collects the English messages the server sends (string literals that are not
logs, SQL or test code) from the non-test Go files and checks that every one of them
is in EN_KEYS (frontend/src/i18n/index.ts) and has a Russian translation in
frontend/locales/ru.yaml. Also reports keys of EN_KEYS without a translation.
Exit status 1 when something is missing.
"""
import glob
import os
import re
import sys

root = os.path.abspath(sys.argv[1] if len(sys.argv) > 1 else os.path.join(os.path.dirname(__file__), '..'))
index_ts = os.path.join(root, 'frontend', 'src', 'i18n', 'index.ts')
ru_yaml = os.path.join(root, 'frontend', 'locales', 'ru.yaml')

STR = r'"((?:[^"\\\n]|\\.)*)"'


def en_keys():
    text = open(index_ts, encoding='utf-8').read()
    block = text[text.index('export const EN_KEYS = ['):]
    block = block[:block.index('] as const;')]
    return [m.group(1) for m in re.finditer(r"'((?:[^'\\\n]|\\.)*)'", block)]


def ru_keys():
    keys = set()
    for line in open(ru_yaml, encoding='utf-8'):
        m = re.match(r'^"((?:[^"\\]|\\.)*)":', line)
        if m:
            keys.add(m.group(1))
    return keys


# Strings that are not interface messages: distribution names, command-line help, protocol
# and service texts, and the start of a message that is built by concatenation.
NOT_MESSAGES = {
    'Alpine Linux', 'Arch Linux', 'Red Hat Enterprise Linux', 'Rocky Linux', 'Void Linux',
    'HTTP listen address', 'SQLite database path', 'YAML configuration file',
    'NetRave virtual network labs', 'Sec-WebSocket-Accept: ', 'QEMU exited unexpectedly: ',
}

SQL = re.compile(r'\b(SELECT|UPDATE|INSERT|DELETE|CREATE|ALTER|DROP|PRAGMA|VALUES|WHERE|FROM|JOIN)\b')


def go_keys():
    found = {}
    for path in glob.glob(os.path.join(root, 'internal', '**', '*.go'), recursive=True):
        if path.endswith('_test.go') or '/labtest/' in path:
            continue
        for lineno, line in enumerate(open(path, encoding='utf-8'), 1):
            code = line.strip()
            if code.startswith('//') or re.search(r'\b(log\.|fmt\.|Errorf|Sprintf|Fatal|Printf)', code):
                continue
            for m in re.finditer(STR, code):
                s = m.group(1)
                if not re.match(r'^[A-Z(«{]', s) or ' ' not in s or '%' in s or '\\n' in s:
                    continue
                if s in NOT_MESSAGES or SQL.search(s) or re.search(r'[<>=;/]', s):
                    continue
                if re.match(r'^(GET|POST|PUT|PATCH|DELETE) ', s):
                    continue
                found.setdefault(s, f'{os.path.relpath(path, root)}:{lineno}')
    return found


def main():
    en = en_keys()
    en_set = set(en)
    ru = ru_keys()
    gk = go_keys()
    ok = True
    missing_en = sorted((k, v) for k, v in gk.items() if k not in en_set)
    if missing_en:
        ok = False
        print('Go keys missing from EN_KEYS (frontend/src/i18n/index.ts):')
        for k, where in missing_en:
            print(f'  {where}: {k}')
    missing_ru = sorted(k for k in en_set if k not in ru)
    if missing_ru:
        ok = False
        print('EN_KEYS without a translation in frontend/locales/ru.yaml:')
        for k in missing_ru:
            print(f'  {k}')
    extra_ru = sorted(k for k in ru if k not in en_set)
    if extra_ru:
        ok = False
        print('ru.yaml keys that are not in EN_KEYS:')
        for k in extra_ru:
            print(f'  {k}')
    print(f'checked {len(gk)} Go messages, {len(en_set)} EN_KEYS, {len(ru)} translations: {"ok" if ok else "FAILED"}')
    return 0 if ok else 1


if __name__ == '__main__':
    sys.exit(main())
