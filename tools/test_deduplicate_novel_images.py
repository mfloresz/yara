"""Synthetic tests only: never open the repository's runtime data directory."""

import importlib.util
import io
import json
import os
from pathlib import Path
import shutil
import sqlite3
import tempfile
import unittest
from contextlib import closing, redirect_stderr, redirect_stdout
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location(
    'deduplicate_novel_images', Path(__file__).with_name('deduplicate-novel-images.py'))
TOOL = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(TOOL)


class RemediationTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.data = self.root / 'data'
        self.data.mkdir()
        self.backup = self.root / 'backup'
        self.paths = []
        with closing(sqlite3.connect(self.data / 'data.db')) as db, db:
            db.executescript('CREATE TABLE _collections (id TEXT, name TEXT);'
                             "INSERT INTO _collections VALUES ('images123', 'novel_images');"
                             'CREATE TABLE novel_images (id TEXT, novel TEXT, file TEXT);')
            for index in range(3):
                record = 'record' + str(index)
                db.execute('INSERT INTO novel_images VALUES (?, ?, ?)', (record, 'novel1', 'image.jpg'))
                path = self.data / 'storage' / 'images123' / record / 'image.jpg'
                path.parent.mkdir(parents=True)
                path.write_bytes(b'\xff\xd8synthetic identical image\xff\xd9')
                path.chmod(0o644)
                timestamp = 1700000000000000000 + index * 1000000000
                os.utime(path, ns=(timestamp, timestamp))
                self.paths.append(path)
        # A full backup must also include non-image files and empty directories.
        (self.data / 'app.key').write_bytes(b'synthetic-test-key-not-a-real-key')
        (self.data / 'empty-directory').mkdir()

    def copy_backup(self):
        shutil.copytree(self.data, self.backup)

    def apply(self):
        return TOOL.run(self.data, apply=True, offline=True, backup=self.backup)

    def state(self):
        return {str(p.relative_to(self.data)): (p.read_bytes(), p.stat().st_ino,
                p.stat().st_mtime_ns, p.stat().st_mode)
                for p in self.data.rglob('*') if p.is_file()}

    def test_dry_run_is_default_and_never_writes(self):
        before = self.state()
        with redirect_stdout(io.StringIO()) as output:
            self.assertEqual(TOOL.main(['--data-dir', str(self.data)]), 0)
        report = json.loads(output.getvalue())
        self.assertEqual(report['mode'], 'dry-run')
        self.assertEqual(report['candidates'], 2)
        self.assertEqual(report['linked'], 0)
        self.assertEqual(report['duplicate_bytes'], 2 * self.paths[0].stat().st_size)
        self.assertEqual(report['estimated_reclaimable_bytes'],
                         sum(p.stat().st_blocks * 512 for p in self.paths[1:]))
        self.assertEqual(before, self.state())
        self.assertFalse((self.data / 'data.db-shm').exists())
        self.assertFalse((self.data / 'data.db-wal').exists())
        self.assertEqual(TOOL.run(self.data)['candidates'], 2)

    def test_apply_and_both_modes_are_idempotent(self):
        self.copy_backup()
        original_db = (self.data / 'data.db').read_bytes()
        old_metadata = [TOOL.metadata(p, p.stat()) for p in self.paths]
        old_mtimes = [p.stat().st_mtime_ns for p in self.paths]
        self.assertEqual(len(set(old_mtimes)), 3)
        report = self.apply()
        self.assertEqual(report['linked'], 2)
        self.assertEqual(report['skipped'], 0)
        self.assertEqual(len({p.stat().st_ino for p in self.paths}), 1)
        self.assertEqual(original_db, (self.data / 'data.db').read_bytes())
        self.assertEqual(old_metadata, [TOOL.metadata(p, p.stat()) for p in self.paths])
        self.assertEqual([p.stat().st_mtime_ns for p in self.paths], [old_mtimes[0]] * 3)
        self.assertEqual([ (self.backup / p.relative_to(self.data)).stat().st_mtime_ns
                           for p in self.paths], old_mtimes)
        for report in (TOOL.run(self.data), self.apply()):
            self.assertEqual(report['candidates'], 0)
            self.assertEqual(report['linked'], 0)
            self.assertEqual(report['already_linked'], 2)
            self.assertEqual(report['duplicate_bytes'], 0)
            self.assertEqual(report['estimated_reclaimable_bytes'], 0)
        self.assertEqual(list(self.data.rglob('.dedup-*')), [])

    def test_preexisting_hardlinks_are_not_reclaimable_duplicates(self):
        self.paths[1].unlink()
        os.link(self.paths[0], self.paths[1])
        report = TOOL.run(self.data)
        self.assertEqual(report['already_linked'], 1)
        self.assertEqual(report['candidates'], 1)
        self.assertEqual(report['duplicate_bytes'], self.paths[2].stat().st_size)
        self.assertEqual(report['estimated_reclaimable_bytes'], self.paths[2].stat().st_blocks * 512)
        self.copy_backup()
        self.assertEqual(self.apply()['linked'], 1)
        report = TOOL.run(self.data)
        self.assertEqual(report['already_linked'], 2)
        self.assertEqual(report['estimated_reclaimable_bytes'], 0)

    def test_custom_data_directory_cli_audit_and_apply(self):
        custom = self.root / 'instance-custom' / 'pocketbase-files'
        custom.parent.mkdir()
        self.data.rename(custom)
        self.data = custom
        self.paths = [custom / 'storage' / 'images123' / ('record' + str(i)) / 'image.jpg'
                      for i in range(3)]
        with redirect_stdout(io.StringIO()) as output:
            status = TOOL.main(['--data-dir', str(custom)])
        self.assertEqual(status, 0)
        self.assertEqual(json.loads(output.getvalue())['candidates'], 2)
        self.copy_backup()
        with redirect_stdout(io.StringIO()) as output:
            status = TOOL.main(['--data-dir', str(custom), '--backup-dir', str(self.backup),
                                '--offline', '--apply'])
        self.assertEqual(status, 0)
        self.assertEqual(json.loads(output.getvalue())['linked'], 2)
        self.assertEqual(len({p.stat().st_ino for p in self.paths}), 1)
        self.assertFalse((self.root / 'data').exists())

    def test_deleting_canonical_record_leaves_other_urls_and_backup(self):
        self.copy_backup()
        contents = self.paths[0].read_bytes()
        self.apply()
        shutil.rmtree(self.paths[0].parent)
        self.assertEqual(self.paths[1].read_bytes(), contents)
        self.assertEqual(self.paths[2].read_bytes(), contents)
        self.assertEqual(self.paths[1].stat().st_nlink, 2)
        for path in self.paths:
            saved = self.backup / path.relative_to(self.data)
            self.assertEqual(saved.read_bytes(), contents)
            self.assertNotEqual(saved.stat().st_ino, self.paths[1].stat().st_ino)

    def test_no_cross_novel_deduplication(self):
        with closing(sqlite3.connect(self.data / 'data.db')) as db, db:
            db.execute("UPDATE novel_images SET novel = 'other' WHERE id = 'record2'")
        self.copy_backup()
        report = self.apply()
        self.assertEqual(report['linked'], 1)
        self.assertNotEqual(self.paths[0].stat().st_ino, self.paths[2].stat().st_ino)

    def test_different_content_is_not_linked_even_with_hash_collision(self):
        self.paths[2].write_bytes(b'X' * self.paths[2].stat().st_size)
        os.utime(self.paths[2], ns=(1700000000000000000, 1700000000000000000))
        self.copy_backup()
        with patch.object(TOOL, 'digest', return_value=b'pretend-collision'):
            report = self.apply()
        self.assertEqual(report['linked'], 1)
        self.assertNotEqual(self.paths[0].stat().st_ino, self.paths[2].stat().st_ino)

    def test_permissions_mismatch_is_skipped_but_mtime_is_allowed(self):
        self.paths[1].chmod(0o600)
        os.utime(self.paths[2], ns=(1800000000000000000, 1800000000000000000))
        report = TOOL.run(self.data)
        self.assertEqual(report['candidates'], 1)
        self.assertEqual(report['skip_reasons']['metadata_device_or_content_mismatch'], 1)

    def test_xattr_mismatch_is_skipped(self):
        if not hasattr(os, 'setxattr'):
            self.skipTest('xattrs unavailable')
        try:
            os.setxattr(self.paths[2], 'user.dedup-test', b'different')
        except OSError as exc:
            self.skipTest(str(exc))
        report = TOOL.run(self.data)
        self.assertEqual(report['candidates'], 1)
        self.assertEqual(report['skipped'], 1)

    def test_missing_and_symlink_files_are_reported(self):
        self.paths[1].unlink()
        self.paths[2].unlink()
        self.paths[2].symlink_to(self.paths[0])
        report = TOOL.run(self.data)
        self.assertEqual(report['candidates'], 0)
        self.assertEqual(report['skip_reasons'], {'FileNotFoundError': 1, 'symlink': 1})

    def test_symlink_parent_is_not_followed(self):
        original = self.paths[2].parent
        moved = self.root / 'moved'
        original.rename(moved)
        original.symlink_to(moved, target_is_directory=True)
        report = TOOL.run(self.data)
        self.assertEqual(report['skip_reasons'], {'symlink': 1})

    def test_non_regular_file_is_not_opened(self):
        self.paths[2].unlink()
        os.mkfifo(self.paths[2])
        report = TOOL.run(self.data)
        self.assertEqual(report['skip_reasons'], {'not_regular': 1})

    def test_path_traversal_is_rejected(self):
        with closing(sqlite3.connect(self.data / 'data.db')) as db, db:
            db.execute("UPDATE novel_images SET file = '../escape' WHERE id = 'record2'")
        self.assertEqual(TOOL.run(self.data)['skip_reasons'], {'invalid_path_component': 1})

    def test_offline_acknowledgement_and_backup_required(self):
        before = self.state()
        with self.assertRaisesRegex(TOOL.SafetyError, '--offline'):
            TOOL.run(self.data, apply=True)
        with self.assertRaisesRegex(TOOL.SafetyError, '--backup-dir'):
            TOOL.run(self.data, apply=True, offline=True)
        self.assertEqual(before, self.state())

    def test_backup_must_be_existing_complete_and_external(self):
        for backup in (self.backup, self.data, self.data / 'backup', self.root):
            with self.subTest(backup=backup), self.assertRaises((TOOL.SafetyError, OSError)):
                TOOL.run(self.data, apply=True, offline=True, backup=backup)
        self.copy_backup()
        (self.backup / 'app.key').unlink()
        with self.assertRaisesRegex(TOOL.SafetyError, 'complete'):
            self.apply()

    def test_backup_requires_empty_directories_too(self):
        self.copy_backup()
        (self.backup / 'empty-directory').rmdir()
        with self.assertRaisesRegex(TOOL.SafetyError, 'complete'):
            self.apply()

    def test_backup_mismatch_is_rejected_before_any_changes(self):
        self.copy_backup()
        (self.backup / 'app.key').write_bytes(b'wrong-test-key')
        before = self.state()
        with self.assertRaisesRegex(TOOL.SafetyError, 'differs'):
            self.apply()
        self.assertEqual(before, self.state())

    def test_hardlinked_backup_rejected_even_cross_filename(self):
        self.copy_backup()
        path = self.backup / 'app.key'
        path.unlink()
        os.link(self.paths[0], path)
        with self.assertRaisesRegex(TOOL.SafetyError, 'must not hardlink'):
            self.apply()

    def test_symlink_backup_rejected(self):
        self.copy_backup()
        key = self.backup / 'app.key'
        key.unlink()
        key.symlink_to(self.data / 'app.key')
        with self.assertRaisesRegex(TOOL.SafetyError, 'symlink'):
            self.apply()

    def test_symlink_database_rejected(self):
        moved = self.root / 'database'
        (self.data / 'data.db').rename(moved)
        (self.data / 'data.db').symlink_to(moved)
        with self.assertRaisesRegex(TOOL.SafetyError, 'symlink'):
            TOOL.run(self.data)

    def test_nonempty_wal_and_journal_rejected_without_creating_shm(self):
        for suffix in ('-wal', '-journal'):
            sidecar = self.data / ('data.db' + suffix)
            sidecar.write_bytes(b'pending transaction')
            with self.assertRaisesRegex(TOOL.SafetyError, 'checkpoint offline'):
                TOOL.run(self.data)
            sidecar.unlink()
        self.assertFalse((self.data / 'data.db-shm').exists())

    def test_failed_replace_cleans_temporary_link_and_reports_partial(self):
        self.copy_backup()
        with patch.object(TOOL.os, 'replace', side_effect=OSError('synthetic failure')):
            report = self.apply()
        self.assertEqual(report['linked'], 0)
        self.assertEqual(report['skipped'], 2)
        self.assertEqual(len({p.stat().st_ino for p in self.paths}), 3)
        self.assertEqual(list(self.data.rglob('.dedup-*')), [])

    def test_directory_sync_failure_counts_actual_replacements(self):
        self.copy_backup()
        with patch.object(TOOL.os, 'fsync', side_effect=OSError('synthetic sync failure')):
            report = self.apply()
        self.assertEqual(report['linked'], 2)
        self.assertEqual(report['skip_reasons'], {'directory_sync_failed_after_replace': 2})
        self.assertEqual(len({p.stat().st_ino for p in self.paths}), 1)

    def test_temporary_name_collision_does_not_remove_existing_file(self):
        source, target = self.paths[:2]
        collision = target.with_name('.dedup-collision')
        collision.write_bytes(b'not ours')
        with patch.object(TOOL.uuid, 'uuid4') as identifier:
            identifier.return_value.hex = 'collision'
            with self.assertRaises(FileExistsError):
                TOOL.replace_link(source, target, TOOL.snapshot(source.stat()), TOOL.snapshot(target.stat()))
        self.assertEqual(collision.read_bytes(), b'not ours')
        self.assertNotEqual(source.stat().st_ino, target.stat().st_ino)

    def test_backup_inventory_does_not_ignore_walk_permission_errors(self):
        self.copy_backup()
        def denied(root, **kwargs):
            kwargs['onerror'](PermissionError('synthetic inaccessible directory'))
            return iter(())
        with patch.object(TOOL.os, 'walk', side_effect=denied):
            with self.assertRaises(PermissionError):
                self.apply()

    def test_changed_target_is_not_replaced(self):
        source, target = self.paths[:2]
        expected_source = TOOL.snapshot(source.stat())
        expected_target = TOOL.snapshot(target.stat())
        target.write_bytes(b'changed')
        with self.assertRaisesRegex(TOOL.SafetyError, 'file_changed'):
            TOOL.replace_link(source, target, expected_source, expected_target)
        self.assertEqual(target.read_bytes(), b'changed')
        self.assertEqual(list(self.data.rglob('.dedup-*')), [])

    def test_cli_refusal_and_skipped_status(self):
        with redirect_stderr(io.StringIO()):
            self.assertEqual(TOOL.main(['--data-dir', str(self.data), '--apply']), 1)
        self.paths[2].unlink()
        with redirect_stdout(io.StringIO()):
            self.assertEqual(TOOL.main(['--data-dir', str(self.data)]), 2)


if __name__ == '__main__':
    unittest.main()
