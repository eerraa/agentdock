#!/usr/bin/env python3
"""Provision explicit browser permissions only in the disposable image-test container.

The public smoke client remains read-only with respect to policy. This fixture
first verifies the default pending/reject contract, grants only browser start
and close for this otherwise empty container, and restores/verifies the default
policy afterward. No approval is accepted and no request is replayed.
"""
import json
import os
import re
import sys
import urllib.request
from pathlib import Path

RECEIPT = Path('/tmp/agentdock-browser-smoke-policy.json')
START = {
    'action': 'start',
    'headless': True,
    'url': 'data:text/html,<title>AgentDock Browser Smoke</title><main>browser-ok</main>',
    'timeout_ms': 30000,
}


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def main(phase, container_id):
    require(phase in ('prepare', 'restore'), 'unknown fixture phase')
    require(re.fullmatch(r'[0-9a-f]{64}', container_id), 'expected the exact Docker container identity')
    require(os.environ.get('HOSTNAME') == container_id[:12], 'not the selected disposable container')
    require(os.getuid() == 10001 and os.environ.get('HOME') == '/home/agentdock', 'unexpected image-test user')
    token = os.environ.get('AGENTDOCK_AUTH_TOKEN', '')
    require(bool(token), 'authenticated local management is required')

    def request(path, body=None):
        data = None if body is None else json.dumps(body).encode('utf-8')
        headers = {'Authorization': 'Bearer ' + token, 'Accept': 'application/json, text/event-stream'}
        if data is not None:
            headers['Content-Type'] = 'application/json'
        req = urllib.request.Request('http://127.0.0.1:8765' + path, data=data, headers=headers)
        with urllib.request.urlopen(req, timeout=30) as response:
            return json.load(response)

    def check_default_reject():
        envelope = request('/mcp', {'jsonrpc': '2.0', 'id': 91, 'method': 'tools/call',
                                   'params': {'name': 'browser_session', 'arguments': START}})
        require('error' not in envelope, 'browser permission probe returned an RPC error')
        result = envelope['result']
        require(result.get('isError', False) is False, 'browser probe returned a tool error')
        pending = result['structuredContent']
        require(pending.get('status') == 'pending_approval' and pending.get('executed') is False,
                'default policy did not block browser dispatch')
        require(pending['permission']['mode'] == 'rules', 'default rules mode changed')
        approval_id, call_id = pending['approval_id'], pending['call_id']
        require(re.fullmatch(r'approval_[A-Za-z0-9_-]+', approval_id), 'invalid approval identity')
        require(re.fullmatch(r'call_[A-Za-z0-9_-]+', call_id), 'invalid call identity')
        detail = request('/internal/runtime/approvals/' + approval_id)
        require(detail.get('request_available') is True and json.loads(detail['fixed_request']) == START,
                'the pending request differs from the fixed browser probe')
        require(detail['approval']['dispatch_count'] == 0, 'unapproved browser was dispatched')
        rejected = request('/internal/runtime/approvals/' + approval_id + '/reject', {})
        require(rejected.get('dispatched') is False and rejected['approval']['status'] == 'rejected',
                'rejection did not settle the fixed request without dispatch')
        call = request('/internal/runtime/calls/' + call_id)
        require(call['status'] == 'cancelled', 'rejected request was not recorded as cancelled')

    policy = request('/internal/runtime/permissions/effective')['policy']
    if phase == 'prepare':
        require(not RECEIPT.exists(), 'a previous fixture receipt exists; do not repeat the grant')
        require(policy['global_mode'] == 'rules' and not policy['scopes'] and not policy['rules'],
                'the disposable test container must start with the unmodified default policy')
        check_default_reject()
        rules = [{'id': 'image-smoke-' + action, 'tool': 'browser_session', 'action': action,
                  'effect': 'allow', 'reason': 'Explicit disposable image-test browser permission'}
                 for action in ('start', 'close')]
        changed = request('/internal/runtime/permissions', {
            'scope': 'global', 'expected_revision': policy['revision'], 'rules': rules})['policy']
        require(changed['global_mode'] == policy['global_mode'] and changed['scopes'] == policy['scopes']
                and changed['rules'] == rules, 'fixture changed permissions outside the two browser actions')
        receipt = {'original': policy, 'granted': changed, 'container_id': container_id}
        with RECEIPT.open('x', encoding='utf-8') as stream:
            os.chmod(RECEIPT, 0o600)
            json.dump(receipt, stream)
        print('Default browser approval/rejection verified; two disposable-container rules granted.')
    else:
        receipt = json.loads(RECEIPT.read_text(encoding='utf-8'))
        require(receipt['container_id'] == container_id and policy == receipt['granted'],
                'fixture policy changed unexpectedly; do not overwrite it')
        original = receipt['original']
        restored = request('/internal/runtime/permissions', {
            'scope': 'global', 'expected_revision': policy['revision'],
            'rules': [] if original['rules'] is None else original['rules']})['policy']
        # Go serializes a restored empty rule slice as null rather than [].
        # Accept only those two equivalent empty representations; non-empty
        # policies must still match exactly before testing non-dispatch again.
        same_rules = restored['rules'] == original['rules'] or (
            restored['rules'] in (None, []) and original['rules'] in (None, []))
        require(restored['global_mode'] == original['global_mode'] and restored['scopes'] == original['scopes']
                and same_rules, 'original fixture policy was not restored')
        check_default_reject()
        print('Original rules restored; browser start again requires approval and rejection prevents dispatch.')


if __name__ == '__main__':
    if len(sys.argv) != 3:
        raise SystemExit('usage: docker-smoke-permissions.py prepare|restore CONTAINER_ID')
    main(sys.argv[1], sys.argv[2])
