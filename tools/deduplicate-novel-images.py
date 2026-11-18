#!/usr/bin/env python3
"""Offline, local-only PocketBase novel_images hardlink remediation (stdlib)."""

import argparse
from collections import Counter
from contextlib import closing, contextmanager
import hashlib
import json
import os
from pathlib import Path
import sqlite3
import stat
import sys
import uuid


class SafetyError(Exception):
    """Refuse a filesystem or database layout that cannot be handled safely."""


class AppliedError(SafetyError):
    """Replacement happened, but syncing its directory failed."""


def snapshot(info):
    return (info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns,
            info.st_ctime_ns, info.st_mode, info.st_uid, info.st_gid)


def regular(path):
    """Reject symlinks at every component, including the data directory."""
    path = Path(os.path.abspath(path))
    for component in reversed((path, *path.parents)):
        info = component.lstat()
        if stat.S_ISLNK(info.st_mode):
            raise SafetyError("symlink")
    info = path.lstat()
    if not stat.S_ISREG(info.st_mode):
        raise SafetyError("not_regular")
    return info


@contextmanager
def opened(path):
    before = regular(path)
    fd = os.open(path, os.O_RDONLY | getattr(os, 'O_NOFOLLOW', 0))
    with os.fdopen(fd, 'rb') as stream:
        if snapshot(os.fstat(stream.fileno())) != snapshot(before):
            raise SafetyError("file_changed")
        yield stream
        if snapshot(os.fstat(stream.fileno())) != snapshot(before):
            raise SafetyError("file_changed")


def digest(path):
    value = hashlib.sha256()
    with opened(path) as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b''):
            value.update(chunk)
    return value.digest()


def equal(left, right):
    with opened(left) as a, opened(right) as b:
        while True:
            chunk = a.read(1024 * 1024)
            if chunk != b.read(1024 * 1024):
                return False
            if not chunk:
                return True


def metadata(path, info):
    # Hardlinks share inode metadata. Preserve compatible access metadata;
    # mtime may differ and becomes the canonical file's mtime only on apply.
    # atime/ctime cannot be preserved and are intentionally excluded.
    attrs = ()
    if hasattr(os, 'listxattr'):
        attrs = tuple(sorted((key, os.getxattr(path, key, follow_symlinks=False))
                             for key in os.listxattr(path, follow_symlinks=False)))
    return (stat.S_IMODE(info.st_mode), info.st_uid, info.st_gid, attrs)


def segment(value):
    if not isinstance(value, str) or not value or value in ('.', '..') or any(
            char in value for char in ('/', '\\', '\x00')):
        raise SafetyError('invalid_path_component')
    return value


def clean_sqlite(data):
    for suffix in ('-wal', '-journal'):
        path = data / ('data.db' + suffix)
        if path.exists() or path.is_symlink():
            if regular(path).st_size:
                raise SafetyError('SQLite WAL/journal is not empty; checkpoint offline before auditing')
    regular(data / 'data.db')


def records(data):
    clean_sqlite(data)
    # immutable=1 prevents even SQLite lock/SHM creation. WAL is rejected above,
    # not silently ignored. Never open a writable connection, including dry-run.
    uri = (data / 'data.db').as_uri() + '?mode=ro&immutable=1'
    with closing(sqlite3.connect(uri, uri=True)) as db:
        found = db.execute('SELECT id FROM _collections WHERE name = ?',
                           ('novel_images',)).fetchall()
        if len(found) != 1:
            raise SafetyError('Expected exactly one novel_images collection')
        collection = segment(found[0][0])
        rows = db.execute('SELECT id, novel, file FROM novel_images ORDER BY novel, id').fetchall()
    return collection, rows


def tree(root):
    """Inventory all of data (not just images); never follow symlinks."""
    if root.is_symlink() or not root.is_dir():
        raise SafetyError('Backup and data must be real directories')
    # Also reject symlinks in the root's ancestors.
    for part in root.parents:
        if part.is_symlink():
            raise SafetyError('symlink')
    files, directories = {}, set()
    def walk_error(error):
        raise error

    for base, dirs, names in os.walk(root, followlinks=False, onerror=walk_error):
        for name in dirs:
            path = Path(base) / name
            if path.is_symlink():
                raise SafetyError('Backup/data tree contains a symlink')
            directories.add(path.relative_to(root))
        for name in names:
            path = Path(base) / name
            files[path.relative_to(root)] = regular(path)
    return files, directories


def verify_backup(data, backup):
    if backup is None:
        raise SafetyError('--backup-dir is required for --apply')
    backup = Path(os.path.abspath(backup))
    if data == backup or data in backup.parents or backup in data.parents:
        raise SafetyError('Backup must be outside data and must not contain data')
    originals, dirs = tree(data)
    copies, copy_dirs = tree(backup)
    if originals.keys() != copies.keys() or dirs != copy_dirs:
        raise SafetyError('Backup must contain a complete, unchanged copy of data')
    original_inodes = {(s.st_dev, s.st_ino) for s in originals.values()}
    for name, info in originals.items():
        saved = copies[name]
        if (saved.st_dev, saved.st_ino) in original_inodes:
            raise SafetyError('Backup must not hardlink any original file')
        if info.st_size != saved.st_size or digest(data / name) != digest(backup / name) or not equal(data / name, backup / name):
            raise SafetyError('Backup differs from data: ' + str(name))


def replace_link(source, target, expected_source, expected_target):
    temporary = target.with_name('.dedup-' + uuid.uuid4().hex)
    created = False
    try:
        if snapshot(regular(source)) != expected_source or snapshot(regular(target)) != expected_target:
            raise SafetyError('file_changed')
        os.link(source, temporary, follow_symlinks=False)
        created = True
        linked = regular(temporary)
        # Creating a link changes ctime, so compare the identity and content state
        # separately after link creation. Offline operation is still mandatory.
        if (linked.st_dev, linked.st_ino) != expected_source[:2]:
            raise SafetyError('file_changed')
        if snapshot(regular(target)) != expected_target or not equal(temporary, target):
            raise SafetyError('file_changed')
        os.replace(temporary, target)
        created = False
        try:
            fd = os.open(target.parent, os.O_RDONLY | getattr(os, 'O_DIRECTORY', 0))
            try:
                os.fsync(fd)
            finally:
                os.close(fd)
        except OSError as exc:
            raise AppliedError('directory_sync_failed_after_replace') from exc
    finally:
        if created:
            temporary.unlink(missing_ok=True)


def run(data, *, apply=False, offline=False, backup=None):
    data = Path(os.path.abspath(data))
    if apply and not offline:
        raise SafetyError('--apply requires --offline acknowledgement (all writers stopped)')
    collection, rows = records(data)
    if apply:
        verify_backup(data, backup)
    report = dict(mode='apply' if apply else 'dry-run', records=len(rows), files=0,
                  logical_bytes=0, candidates=0, linked=0, already_linked=0,
                  duplicate_bytes=0, estimated_reclaimable_bytes=0, skipped=0,
                  skip_reasons={})
    reasons = Counter()
    groups = {}
    for record, novel, filename in rows:
        try:
            segment(record)
            segment(novel)
            segment(filename)
            path = data / 'storage' / collection / record / filename
            info = regular(path)
            file_metadata = metadata(path, info)
            content_hash = digest(path)
            if snapshot(regular(path)) != snapshot(info):
                raise SafetyError('file_changed')
            report['files'] += 1
            report['logical_bytes'] += info.st_size
            key = (novel, info.st_size, content_hash)
            peers = groups.setdefault(key, [])
            compatible = None
            for peer in peers:
                peer_info = regular(peer)
                if (peer_info.st_dev, peer_info.st_ino) == (info.st_dev, info.st_ino):
                    report['already_linked'] += 1
                    compatible = False
                    break
                if (peer_info.st_dev == info.st_dev and
                        metadata(peer, peer_info) == file_metadata and equal(peer, path)):
                    compatible = (peer, peer_info)
                    break
            if compatible is False:
                continue
            if compatible is None:
                if peers:
                    reasons['metadata_device_or_content_mismatch'] += 1
                peers.append(path)
                continue
            peer, peer_info = compatible
            report['candidates'] += 1
            report['duplicate_bytes'] += info.st_size
            # An inode with other links might not release any blocks. This is
            # deliberately a conservative estimate, not a promise of disk space.
            if info.st_nlink == 1:
                report['estimated_reclaimable_bytes'] += getattr(info, 'st_blocks', 0) * 512
            if apply:
                replace_link(peer, path, snapshot(peer_info), snapshot(info))
                report['linked'] += 1
        except (OSError, SafetyError) as exc:
            # A refusal is visible; never fall back to copying or changing DB rows.
            if isinstance(exc, AppliedError):
                report['linked'] += 1
            reason = str(exc) if isinstance(exc, SafetyError) else type(exc).__name__
            reasons[reason] += 1
    report['skipped'] = sum(reasons.values())
    report['skip_reasons'] = dict(sorted(reasons.items()))
    return report


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--data-dir', required=True, type=Path)
    parser.add_argument('--apply', action='store_true', help='Replace duplicates with local hardlinks')
    parser.add_argument('--offline', action='store_true', help='Acknowledge that ALL data writers are stopped')
    parser.add_argument('--backup-dir', type=Path, help='Existing independent full offline copy of data')
    args = parser.parse_args(argv)
    try:
        report = run(args.data_dir, apply=args.apply, offline=args.offline, backup=args.backup_dir)
    except (SafetyError, OSError, sqlite3.Error) as exc:
        print('Refused: ' + str(exc), file=sys.stderr)
        return 1
    print(json.dumps(report, indent=2, sort_keys=True))
    return 2 if report['skipped'] else 0


if __name__ == '__main__':
    sys.exit(main())
