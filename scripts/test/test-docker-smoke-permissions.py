#!/usr/bin/env python3
"""Behavioral tests for the disposable Docker permission fixture; no live I/O."""
import copy
import io
import json
import os
import tempfile
import types
import unittest
from pathlib import Path
from unittest.mock import patch

HELPER = Path(__file__).with_name('docker-smoke-permissions.py')
CONTAINER = 'a' * 64


class PermissionFixtureTests(unittest.TestCase):
    def setUp(self):
        self.module = types.ModuleType('image_permission_fixture')
        exec(compile(HELPER.read_text(encoding='utf-8'), str(HELPER), 'exec'), self.module.__dict__)
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.module.RECEIPT = Path(self.temp.name) / 'receipt.json'
        self.policy = {'schema_version': 1, 'revision': 1, 'global_mode': 'rules',
                       'scopes': [], 'rules': [], 'updated_at': 'fixture'}
        self.writes = []
        self.reads = []
        self.tamper = False
        self.dispatch = False
        self.counter = 0
        self.enter = self.enterContext
        self.enter(patch.dict(os.environ, {'HOSTNAME': CONTAINER[:12], 'HOME': '/home/agentdock',
                                          'AGENTDOCK_AUTH_TOKEN': 'test-only'}, clear=True))
        self.enter(patch.object(os, 'getuid', return_value=10001, create=True))
        self.enter(patch('urllib.request.urlopen', side_effect=self.respond))

    def respond(self, request, timeout):
        self.assertEqual(timeout, 30)
        self.assertEqual(request.get_header('Authorization'), 'Bearer test-only')
        self.assertTrue(request.full_url.startswith('http://127.0.0.1:8765/'))
        path = request.full_url.removeprefix('http://127.0.0.1:8765')
        body = None if request.data is None else json.loads(request.data)
        self.reads.append(path)
        if path == '/internal/runtime/permissions/effective':
            value = {'policy': copy.deepcopy(self.policy)}
        elif path == '/internal/runtime/permissions':
            self.assertEqual(set(body), {'scope', 'expected_revision', 'rules'})
            self.assertEqual(body['expected_revision'], self.policy['revision'])
            self.assertIsInstance(body['rules'], list)
            self.writes.append(copy.deepcopy(body))
            self.policy['rules'] = body['rules'] or None
            self.policy['revision'] += 1
            value = {'policy': copy.deepcopy(self.policy)}
        elif path == '/mcp':
            self.assertEqual(body['params'], {'name': 'browser_session', 'arguments': self.module.START})
            self.assertIn(self.policy['rules'], (None, []))
            self.counter += 1
            value = {'result': {'structuredContent': {'status': 'pending_approval',
                     'executed': self.dispatch, 'permission': {'mode': 'rules'},
                     'approval_id': 'approval_fixture_' + str(self.counter),
                     'call_id': 'call_fixture_' + str(self.counter)}}}
        elif path.endswith('/reject'):
            self.assertEqual(body, {})
            value = {'dispatched': False, 'approval': {'status': 'rejected'}}
        elif path.startswith('/internal/runtime/approvals/'):
            fixed = {} if self.tamper else self.module.START
            value = {'request_available': True, 'fixed_request': json.dumps(fixed),
                     'approval': {'dispatch_count': 0}}
        elif path.startswith('/internal/runtime/calls/'):
            value = {'status': 'cancelled'}
        else:
            self.fail('Unexpected fixture request: ' + path)
        return io.BytesIO(json.dumps(value).encode('utf-8'))

    def test_grant_and_restore_preserve_default_gate(self):
        self.module.main('prepare', CONTAINER)
        self.assertEqual({r['action'] for r in self.policy['rules']}, {'start', 'close'})
        self.assertTrue(all(r['tool'] == 'browser_session' for r in self.policy['rules']))
        self.module.main('restore', CONTAINER)
        self.assertIsNone(self.policy['rules'])
        self.assertEqual(self.policy['global_mode'], 'rules')
        self.assertEqual(self.counter, 2)
        self.assertEqual(len(self.writes), 2)
        self.assertFalse(any('/approve' in path for path in self.reads))

    def test_nil_original_rules_restore_via_explicit_empty_change(self):
        self.policy['rules'] = None
        self.module.main('prepare', CONTAINER)
        self.module.main('restore', CONTAINER)
        self.assertIsNone(self.policy['rules'])
        self.assertEqual(self.writes[-1]['rules'], [])
        self.assertEqual(self.counter, 2)

    def test_wrong_container_does_not_contact_runtime(self):
        with self.assertRaises(RuntimeError):
            self.module.main('prepare', 'b' * 64)
        self.assertEqual(self.reads, [])

    def test_existing_rules_are_not_overwritten(self):
        self.policy['rules'] = [{'id': 'user-rule'}]
        with self.assertRaises(RuntimeError):
            self.module.main('prepare', CONTAINER)
        self.assertEqual(self.writes, [])

    def test_changed_policy_is_not_restored_blindly(self):
        self.module.main('prepare', CONTAINER)
        self.policy['revision'] += 1
        with self.assertRaises(RuntimeError):
            self.module.main('restore', CONTAINER)
        self.assertEqual(len(self.writes), 1)

    def test_changed_fixed_request_blocks_grant(self):
        self.tamper = True
        with self.assertRaises(RuntimeError):
            self.module.main('prepare', CONTAINER)
        self.assertEqual(self.writes, [])

    def test_unapproved_dispatch_blocks_grant(self):
        self.dispatch = True
        with self.assertRaises(RuntimeError):
            self.module.main('prepare', CONTAINER)
        self.assertEqual(self.writes, [])


if __name__ == '__main__':
    unittest.main()
