#!/usr/bin/env python3
"""Add clearly marked demo work to an existing local Werkbord installation.

Requires SQLite 3.37+ and an explicit --apply. Backs up Individual's database and
exports Team's current overview before adding data. Never starts an agent or
creates automatic schedules; future dates are planning dates only. Re-running
skips existing demo projects. Credentials are read locally and never printed.
"""
import argparse
import datetime as dt
import hashlib
import json
import os
from pathlib import Path
import sqlite3
import subprocess
import urllib.error
import urllib.request


def api(base, token, method, route, body=None):
    data = None if body is None else json.dumps(body).encode()
    req = urllib.request.Request(base + route, data=data, method=method,
                                 headers={'Authorization': 'Bearer ' + token, 'Content-Type': 'application/json'})
    try:
        with urllib.request.urlopen(req, timeout=30) as response:
            raw = response.read()
            return json.loads(raw) if raw else None
    except urllib.error.HTTPError as e:
        raise RuntimeError(f'{method} {route}: {e.code} {e.read().decode()}') from None


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--apply', action='store_true')
    parser.add_argument('--data-dir', type=Path, default=Path.home() / 'Library/Application Support/werkbord')
    parser.add_argument('--team-key', type=Path, default=Path.home() / 'Library/Application Support/werkbord-team-desktop/access.key')
    args = parser.parse_args()
    if not args.apply:
        parser.error('Pass --apply to add demo projects to your local app.')
    if sqlite3.sqlite_version_info < (3, 37):
        parser.error('Use a Python runtime with SQLite 3.37 or newer.')
    today = dt.date.today()
    now = dt.datetime.now(dt.timezone.utc)
    stamp = now.strftime('%Y%m%dT%H%M%SZ')
    backup = args.data_dir / 'backups' / ('before-demo-' + stamp)
    backup.mkdir(parents=True, mode=0o700)
    token = (args.data_dir / 'token').read_text().strip()
    call = lambda method, route, body=None: api('http://127.0.0.1:7420', token, method, route, body)
    db = sqlite3.connect(args.data_dir / 'werkbord.db', timeout=30)
    db.execute('PRAGMA foreign_keys=ON')
    with sqlite3.connect(backup / 'werkbord.db') as snapshot:
        db.backup(snapshot)
    os.chmod(backup / 'werkbord.db', 0o600)

    def insert(table, values):
        cols = ','.join(values)
        db.execute(f'INSERT INTO {table} ({cols}) VALUES ({",".join("?" for _ in values)})', list(values.values()))

    def event(kind, pid, tid, at, payload, rid=None):
        insert('events', dict(type=kind, project_id=pid, task_id=tid, run_id=rid,
                             created_at=int(at.timestamp()*1000), payload=json.dumps(payload)))

    def date(offset):
        return (today + dt.timedelta(days=offset)).isoformat()

    existing = {p['name'] for p in call('GET', '/api/projects?history=1')['projects']}
    titles = [
        'Map the onboarding journey', 'Audit keyboard navigation', 'Define shared design tokens',
        'Write a quick-start guide', 'Improve search result ranking', 'Fix a flaky regression test',
        'Review mobile layouts', 'Add a weekly progress digest', 'Document the release checklist',
        'Simplify empty-state copy', 'Measure dashboard load time', 'Review accessibility findings',
        'Add saved board filters', 'Summarise customer interviews', 'Polish the project overview',
        'Review error recovery paths', 'Prepare a stakeholder walkthrough', 'Draft the next milestone',
        'Check localisation coverage', 'Triage incoming feedback', 'Refresh the help articles',
        'Test archive and restore', 'Review the next sprint scope', 'Plan the November handoff',
    ]
    projects = [('Demo · Atlas product', True), ('Demo · Studio website', True),
                ('Demo · Autumn launch', False), ('Demo · Customer research', False),
                ('Demo · Operations', False), ('Demo · Spring campaign', False),
                ('Demo · Retired prototype', False)]
    stats = {'individualProjects': 0, 'individualTasks': 0, 'simulatedRuns': 0, 'teamProjects': 0, 'teamTickets': 0}
    labels = call('GET', '/api/labels')['labels']
    label_ids = [x['id'] for x in labels]
    for label, color in [('Demo feature', '#4f46e5'), ('Demo research', '#0d9488'), ('Demo maintenance', '#d97706')]:
        old = next((x for x in labels if x['name'] == label), None)
        if old:
            label_ids.append(old['id'])
        else:
            created = call('POST', '/api/labels', {'name': label, 'color': color, 'description': 'Illustrative demo work'})
            label_ids.append(created['id'])
    demo_root = args.data_dir / 'demo-repositories'
    for pi, (name, repository) in enumerate(projects):
        if name in existing:
            continue
        if repository:
            folder = demo_root / ('atlas-product' if pi == 0 else 'studio-website')
            folder.mkdir(parents=True, exist_ok=True)
            if not (folder / '.git').exists():
                subprocess.run(['git', 'init', '-q', '-b', 'main', str(folder)], check=True)
                (folder / 'README.md').write_text('# ' + name + '\n\nIllustrative demo repository. No agents have run here.\n')
                subprocess.run(['git', '-C', str(folder), 'add', 'README.md'], check=True)
                subprocess.run(['git', '-C', str(folder), '-c', 'user.name=Demo', '-c', 'user.email=demo@example.test', '-c', 'core.hooksPath=/dev/null', 'commit', '-qm', 'Create demo repository'], check=True)
            p = call('POST', '/api/projects', {'name': name, 'path': str(folder)})
        else:
            p = call('POST', '/api/projects', {'name': name, 'kind': 'work'})
        pid = p['id']
        with db:
            for i, title in enumerate(titles):
                state = 'done' if i < 14 else 'review' if i < 17 else 'doing' if i < 20 else 'backlog'
                days_ago = 3 + (i * 3 + pi * 2) % 39
                created = now - dt.timedelta(days=days_ago, hours=i % 7)
                ended = created + dt.timedelta(hours=2+i % 9)
                tid = 'tsk_demo_' + hashlib.sha256((pid+str(i)).encode()).hexdigest()[:16]
                ms = lambda value: int(value.timestamp()*1000)
                planned = -days_ago if i < 20 else 2 + (i - 20) * 3 + pi
                insert('tasks', dict(id=tid, project_id=pid, title=title, description='[Demo] Illustrative work for ' + name.removeprefix('Demo · ') + '.\n\nAcceptance: capture the outcome, review it with the project owner, and record the next step. No agent execution is scheduled.', state=state, position=float(i+1), version=1, created_at=ms(created), updated_at=ms(ended if state == 'done' else now), work_mode=('agent' if repository and i % 3 == 0 else 'hybrid' if repository else 'human'), execution='{}', orchestration='{}', plan_start=date(planned), plan_end='' if i == 23 else date(planned + (i % 3)), milestone=int(i == 23)))
                label_id = label_ids[(i+pi) % len(label_ids)]
                insert('task_labels', dict(task_id=tid, label_id=label_id))
                payload = {'id': tid, 'projectId': pid, 'title': title, 'state': 'backlog', 'version': 1}
                event('task.created', pid, tid, created, payload)
                if state != 'backlog':
                    event('task.updated', pid, tid, created+dt.timedelta(minutes=15), {**payload, 'state': 'doing', 'version': 2})
                    event('task.updated', pid, tid, ended, {**payload, 'state': state, 'version': 3})
                if repository and i < 17:
                    rid = 'run_demo_' + hashlib.sha256(tid.encode()).hexdigest()[:16]
                    failed = i in [4, 9]
                    usage = {'inputTokens': 5000+i*1250, 'outputTokens': 900+i*130, 'cachedTokens': 1800+i*300, 'costUsd': round(.04+i*.013, 3), 'costKind': 'estimated_api_equivalent', 'source': 'demo (simulated)', 'acceptance': 'rejected' if failed else 'accepted'}
                    distribution = {'runnerId': 'demo-local', 'branch': 'demo/task-'+str(i), 'usage': usage}
                    run_state = 'failed' if failed else 'completed'
                    insert('runs', dict(id=rid, task_id=tid, project_id=pid, agent_id='codex' if i%2 else 'claude-code', state=run_state, reason='[Demo] Simulated historical session; no process was started.', created_at=ms(created), updated_at=ms(ended), ended_at=ms(ended), prompt='[Demo] '+title, model='demo-model', attempt=1, distribution=json.dumps(distribution)))
                    event('run.state_changed', pid, tid, created, {'state': 'running'}, rid)
                    event('run.state_changed', pid, tid, ended, {'state': run_state, 'usage': usage}, rid)
                    if not failed:
                        event('agent.completed', pid, tid, ended, {'state': 'completed'}, rid)
                    stats['simulatedRuns'] += 1
                stats['individualTasks'] += 1
            db.execute('UPDATE projects SET created_at=? WHERE id=?', (ms(now-dt.timedelta(days=45)), pid))
        for kind, summary in [('fact', '[Demo] This project has completed discovery and its first milestone.'), ('decision', '[Demo] Keep changes small and review accessibility before the next milestone.'), ('preference', '[Demo] Use a written handoff with decisions and open questions.')]:
            call('POST', f'/api/projects/{pid}/memory', {'kind': kind, 'summary': summary})
        stats['individualProjects'] += 1
    assert db.execute("SELECT count(*) FROM tasks WHERE id LIKE 'tsk_demo_%' AND (json_extract(orchestration,'$.enabled')=1 OR json_extract(orchestration,'$.scheduledAt') IS NOT NULL)").fetchone()[0] == 0
    assert db.execute("SELECT count(*) FROM runs WHERE id LIKE 'run_demo_%' AND state IN ('starting','running','blocked','waiting_for_user')").fetchone()[0] == 0
    db.close()
    print(json.dumps({'individual': stats, 'backup': str(backup)}), flush=True)

    if args.team_key.exists():
        team_token = args.team_key.read_text().strip()
        team = lambda method, route, body=None: api('http://127.0.0.1:7431', team_token, method, '/api/team/v1'+route, body)
        overview = team('GET', '/overview')
        (backup / 'team-overview.json').write_text(json.dumps(overview, indent=2))
        os.chmod(backup / 'team-overview.json', 0o600)
        existing = {p['project']['name']: p for p in overview['projects']}
        team_labels = team('GET', '/labels')['labels']
        team_label_ids = []
        for name, color in [('Demo feature', '#4f46e5'), ('Demo research', '#0d9488'), ('Demo maintenance', '#d97706')]:
            label = next((label for label in team_labels if label['name'] == name), None)
            if label is None:
                label = team('POST', '/labels', {'name': name, 'color': color, 'description': 'Illustrative demo work'})
            team_label_ids.append(label['id'])
        # Board transitions are metadata only. No execution, approval or schedule endpoint is called.
        for pi, name in enumerate(['Demo · Platform delivery', 'Demo · Brand launch', 'Demo · Support operations', 'Demo · Previous campaign']):
            if name in existing:
                continue
            p = team('POST', '/projects', {'name': name, 'description': '[Demo] Sample coordination project with completed work and upcoming plans. No agents are scheduled.'})
            pid = p['id']
            for i, title in enumerate(titles):
                start = -(28-i) if i < 20 or name == 'Demo · Previous campaign' else (i-18)*2+pi
                k = team('POST', f'/projects/{pid}/tickets', {'title': title, 'description': '[Demo] '+name.removeprefix('Demo · ')+'. Historical scenario for exploring boards, project activity and analytics.', 'requirements': 'Capture the outcome and preserve context for the next milestone.', 'workMode': 'human', 'labelIds': [team_label_ids[(i+pi) % len(team_label_ids)]], 'status': 'available', 'plan': {'start': date(start), 'end': date(start+1)}})
                route = f'/projects/{pid}/tickets/{k["id"]}'
                if i < 20 or name == 'Demo · Previous campaign':
                    team('POST', route+'/claim')
                    team('POST', route+'/start-work')
                if i < 17 or name == 'Demo · Previous campaign':
                    team('POST', route+'/submit', {})
                if i < 14 or name == 'Demo · Previous campaign':
                    team('POST', route+'/complete')
                stats['teamTickets'] += 1
            if name == 'Demo · Previous campaign':
                team('PATCH', f'/projects/{pid}', {'archived': True})
            stats['teamProjects'] += 1
            print(json.dumps({'seededTeamProject': name}), flush=True)
    print(json.dumps({'result': stats, 'backup': str(backup)}))


if __name__ == '__main__':
    main()
