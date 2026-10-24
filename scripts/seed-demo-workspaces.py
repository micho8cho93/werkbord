#!/usr/bin/env python3
"""Populate the local generated-workspace library and Team directory with Demo data.

Uses only the authenticated loopback APIs for writes. Requires --apply, snapshots
Individual and asks Team to back up before any sample data is added. No agents,
invitations, schedules or remote connections are created. Safe to rerun: existing
named workspaces/collections/members are preserved and missing sample rows added.
"""
import argparse
import datetime as dt
import json
import os
from pathlib import Path
import sqlite3
import urllib.error
import urllib.request


class Client:
    def __init__(self, port, key, prefix):
        self.base = f'http://127.0.0.1:{port}' + prefix
        self.token = key.read_text().strip()

    def __call__(self, method, route, body=None):
        request = urllib.request.Request(
            self.base + route, method=method,
            data=None if body is None else json.dumps(body).encode(),
            headers={'Authorization': 'Bearer ' + self.token,
                     'Content-Type': 'application/json'})
        try:
            with urllib.request.urlopen(request, timeout=60) as response:
                raw = response.read()
                return json.loads(raw) if raw else None
        except urllib.error.HTTPError as error:
            # Do not echo response bodies: member creation can return a credential.
            raise RuntimeError(f'{method} {route}: HTTP {error.code}') from None

    def items(self, route):
        items, after = [], ''
        while True:
            page = self('GET', route + '?limit=100' + ('&after=' + after if after else ''))
            items.extend(page['items'])
            after = page.get('nextAfter')
            if not after:
                return items


def field(key, name, kind='text', options=None, required=False):
    result = dict(key=key, name=name, type=kind, required=required)
    if options:
        result['options'] = options
    return result


def samples(today):
    """Stable names identify samples; dates follow the explicit local seed date."""
    def date(days):
        return (today + dt.timedelta(days=days)).isoformat()

    def collection(name, fields, rows):
        return dict(name='Demo · ' + name, schemaVersion=1, fields=fields), rows

    title = field('title', 'Title', required=True)
    done = field('done', 'Completed', 'boolean')
    owner = field('owner', 'Owner')
    collections = {}
    collections['focus'] = collection('Focus commitments', [title,
        field('priority', 'Priority', 'select', ['High', 'Medium', 'Low']),
        field('area', 'Area', 'select', ['Build', 'Learn', 'Life']),
        field('due', 'Planned date', 'date'), field('minutes', 'Minutes', 'number'), done], [
        dict(title=t, priority=['High', 'Medium', 'Low'][i % 3], area=['Build', 'Learn', 'Life'][i % 3],
             due=date(i - 3), minutes=[25, 45, 60][i % 3], done=i < 4)
        for i, t in enumerate(['Sketch the settings flow', 'Practise a Spanish conversation',
            'Plan the coastal weekend', 'Clear the feedback inbox', 'Read the keyboard access notes',
            'Book the bicycle service', 'Prototype the command palette', 'Write a learning retrospective',
            'Plan meals for next week', 'Review the search interaction', 'Try a new testing technique',
            'Organise the studio shelf'])])
    collections['habits'] = collection('Weekly rituals', [title,
        field('cadence', 'Cadence', 'select', ['Daily', 'Weekly']),
        field('streak', 'Streak in weeks', 'number'), done], [
        dict(title=t, cadence='Daily' if i < 3 else 'Weekly', streak=[4, 7, 2, 8, 3, 6, 1][i], done=i < 3)
        for i, t in enumerate(['Protect a morning focus hour', 'Walk without a screen',
            'Capture one learning note', 'Review the coming week', 'Back up personal files',
            'Tidy the workbench', 'Make room for a creative experiment'])])
    collections['books'] = collection('Reading shelf', [title, field('author', 'Author'),
        field('format', 'Format', 'select', ['Book', 'Essay', 'Paper']),
        field('status', 'Reading state', 'select', ['To read', 'Reading', 'Finished']),
        field('pages', 'Pages read', 'number'), field('note', 'Reading note'), done], [
        dict(title=t, author=a, format=['Book', 'Essay', 'Paper'][i % 3],
             status=['Finished', 'Reading', 'To read'][i % 3], pages=[180, 42, 0][i % 3],
             note='[Demo] ' + n, done=i % 3 == 0)
        for i, (t, a, n) in enumerate([
            ('The Shape of Everyday Tools', 'Mira Vale', 'Make the frequent action easy to find.'),
            ('Quiet Systems', 'Eli North', 'A good default can remove a whole decision.'),
            ('Notes from a Small Studio', 'June Alder', 'Protect time for experiments.'),
            ('Designing for Attention', 'Samir Reed', 'Put the next useful step beside the evidence.'),
            ('The Patient Programmer', 'Lena Moss', 'Small changes make review easier.'),
            ('A Field Guide to Questions', 'Owen Finch', 'Ask about the last real example.'),
            ('Working in the Open', 'Nia Brook', 'Write down the reason for a decision.'),
            ('Readable Interfaces', 'Theo Hart', 'Labels should survive a narrow screen.'),
            ('The Practice of Noticing', 'Ana Cove', 'Capture observations before interpretation.'),
            ('Local by Design', 'Ivo Birch', 'Keep ownership understandable.'),
            ('Making Space', 'Rae Linden', 'Fewer commitments leave room to think.'),
            ('Experiments in Rhythm', 'Milo Wren', 'Repeat a useful ritual, then review it.')])])
    collections['ideas'] = collection('Reading notes', [title,
        field('topic', 'Topic', 'select', ['Design', 'Engineering', 'Practice']),
        field('note', 'Idea to try'), done], [
        dict(title=t, topic=['Design', 'Engineering', 'Practice'][i % 3], note='[Demo] ' + n, done=i < 3)
        for i, (t, n) in enumerate([
            ('Give defaults a reason', 'Document why a default helps the common case.'),
            ('Review one small change', 'Split the next refactor at a visible behaviour boundary.'),
            ('Keep an observation log', 'Write three concrete observations after a work session.'),
            ('Name the recovery action', 'Replace a vague error with the next useful step.'),
            ('Measure before tuning', 'Record a baseline before changing the slow path.'),
            ('Leave a written handoff', 'Capture the decision and one open question.'),
            ('Test the narrow view', 'Read every label at phone width.'),
            ('Make room for discovery', 'Leave one afternoon free of planned delivery.')])])
    people = ['Sofia Chen', 'Noah Patel', 'Amara Okafor', 'Lucas Martin', 'Elise Laurent', 'Ada Rivera']
    collections['launch'] = collection('Launch deliverables', [title, owner,
        field('status', 'Stage', 'select', ['Planned', 'In progress', 'Review', 'Ready']),
        field('due', 'Due date', 'date'), done], [
        dict(title=t, owner=people[i % 6] + ' (Demo)', status=['Ready', 'Review', 'In progress', 'Planned'][i % 4],
             due=date(i - 2), done=i % 4 == 0)
        for i, t in enumerate(['Confirm the launch story', 'Review the pricing explanation',
            'Prepare the product walkthrough', 'Write the announcement draft', 'Check the support playbook',
            'Validate the migration guide', 'Record a short demo', 'Review accessibility fixes',
            'Prepare customer questions', 'Run the release rehearsal', 'Collect the final approvals',
            'Plan the first-week retrospective'])])
    collections['decisions'] = collection('Launch decisions', [title, owner,
        field('decision', 'Decision and reason'), field('date', 'Recorded', 'date')], [
        dict(title=t, owner=people[i % 6] + ' (Demo)', decision='[Demo] ' + n, date=date(i - 10))
        for i, (t, n) in enumerate([
            ('Rollout window', 'Use a weekday morning so support is available.'),
            ('Release scope', 'Ship the core workflow before optional integrations.'),
            ('Documentation owner', 'Each feature owner reviews its quick-start section.'),
            ('Success measure', 'Track completed onboarding, not page views.'),
            ('Feedback route', 'Collect launch feedback in one triage queue.'),
            ('Accessibility review', 'Include keyboard and screen-reader checks.'),
            ('Rollback rehearsal', 'Practise recovery before the launch review.'),
            ('Follow-up timing', 'Review evidence after the first full week.')])])
    collections['interviews'] = collection('Research interviews', [
        field('participant', 'Participant', required=True), field('company', 'Fictional organisation'),
        field('segment', 'Segment', 'select', ['Independent', 'Small team', 'Studio']),
        field('date', 'Interview date', 'date'), field('insight', 'Observation'), done], [
        dict(participant=p + ' (Demo)', company=c, segment=['Independent', 'Small team', 'Studio'][i % 3],
             date=date(i - 7), insight='[Demo] ' + n, done=i < 8)
        for i, (p, c, n) in enumerate([
            ('Alex Rowan', 'Cedar Workshop', 'Wants one clear view of the next action.'),
            ('Riley Sol', 'Paper Lantern', 'Checks progress from a phone during travel.'),
            ('Morgan Lake', 'Northline Studio', 'Needs a durable handoff after review.'),
            ('Jamie Quinn', 'Moss & Field', 'Keeps research notes beside project decisions.'),
            ('Taylor Reed', 'Little Orbit', 'Prefers visible ownership over extra notifications.'),
            ('Casey Noor', 'Harbour Works', 'Finds unclear labels harder than dense screens.'),
            ('Jordan Ash', 'Common Thread', 'Wants retained records after removing a view.'),
            ('Avery Sage', 'Pinecone Lab', 'Reviews the week before adding new commitments.'),
            ('Robin Wells', 'Silver Fern', 'Upcoming conversation about project setup.'),
            ('Drew Linden', 'Tandem House', 'Upcoming conversation about review habits.'),
            ('Sky Ellis', 'Meadow Office', 'Upcoming conversation about local data.'),
            ('Finley West', 'Copper Finch', 'Upcoming conversation about team handoffs.')])])
    collections['findings'] = collection('Research findings', [title,
        field('theme', 'Theme', 'select', ['Navigation', 'Trust', 'Collaboration', 'Planning']),
        field('confidence', 'Confidence', 'select', ['Emerging', 'Supported', 'Strong']),
        field('mentions', 'Mentions', 'number'), field('next_step', 'Next step')], [
        dict(title=t, theme=['Navigation', 'Trust', 'Collaboration', 'Planning'][i % 4],
             confidence=['Strong', 'Supported', 'Emerging'][i % 3], mentions=2 + i % 7, next_step='[Demo] ' + n)
        for i, (t, n) in enumerate([
            ('The next action should be obvious', 'Test the task entry point with three participants.'),
            ('Local ownership needs plain language', 'Review the data location explanation.'),
            ('A handoff needs its decision history', 'Try a short decision-and-question template.'),
            ('Weekly plans need room to change', 'Prototype rescheduling without losing context.'),
            ('Labels matter more on narrow screens', 'Read the primary routes at phone width.'),
            ('Deletion should explain retained records', 'Review the collection usage preview.'),
            ('Visible ownership reduces interruptions', 'Test owner labels in the review queue.'),
            ('Dates are not automatic starts', 'Compare the wording in planning and scheduling.'),
            ('Search needs recognisable result context', 'Show the parent workspace with each match.'),
            ('Recovery should preserve identity', 'Walk through archive and restore with a user.')])])
    collections['recipes'] = collection('Kitchen experiments', [
        field('dish', 'Dish', required=True), field('category', 'Category', 'select', ['Lunch', 'Dinner', 'Baking']),
        field('minutes', 'Minutes', 'number'), field('favourite', 'Favourite', 'boolean')], [
        dict(dish=t, category=['Lunch', 'Dinner', 'Baking'][i % 3], minutes=20 + i * 5, favourite=i % 3 == 0)
        for i, t in enumerate(['Lemon chickpea bowl', 'Roasted pepper pasta', 'Rosemary focaccia',
            'Herbed lentil salad', 'Mushroom barley pot', 'Orange almond cake', 'Crisp potato wraps', 'Aubergine tray bake'])])
    collections['risks'] = collection('Team risk register', [title, owner,
        field('likelihood', 'Likelihood', 'select', ['Low', 'Medium', 'High']),
        field('impact', 'Impact', 'select', ['Low', 'Medium', 'High']), field('mitigation', 'Mitigation')], [
        dict(title=t, owner=people[i % 6] + ' (Demo)', likelihood=['Low', 'Medium', 'High'][i % 3],
             impact=['High', 'Medium', 'Low'][i % 3], mitigation='[Demo] ' + n)
        for i, (t, n) in enumerate([
            ('Documentation review slips', 'Reserve a review slot before release day.'),
            ('Support questions arrive together', 'Prepare a shared first-response guide.'),
            ('Interview participant cancels', 'Keep two optional follow-up slots.'),
            ('Decision context gets lost', 'Write the reason beside every launch decision.'),
            ('Scope expands late', 'Keep optional work in the next milestone.'),
            ('A key reviewer is away', 'Name a second reviewer for each critical item.'),
            ('Feedback is scattered', 'Use one triage queue and a daily owner.'),
            ('Research sample is too narrow', 'Add a participant from another segment.')])])
    return collections


def designs(ids, team_id):
    def view(key, name, kind, collection, fields, **extra):
        return dict(id=key, name=name, kind=kind, collectionId=ids[collection], fields=fields, **extra)

    def node(key):
        return dict(type='view', viewId=key)

    def grid(*children):
        return dict(type='grid', columns=2, children=list(children))

    def workspace(name, icon, team, intro, views, body):
        return dict(name='Demo · ' + name, icon=icon, uiSchemaVersion=1,
                    visibility=dict(universal=False, individual=not team, teamWorkspaceIds=[team_id] if team else []),
                    collectionIds=list(dict.fromkeys(v['collectionId'] for v in views)), readBindings=[],
                    definition=dict(views=views, layout=dict(type='stack', children=[dict(type='text', text=intro), *body])))

    edit = ['create', 'update', 'delete']
    return [
        workspace('Focus planner', 'check', False, '[Demo] A week with room to build, learn and live. Tick off commitments and protect useful rituals.', [
            view('week', 'Weekly commitments', 'checklist', 'focus', ['title', 'priority', 'area', 'due', 'minutes', 'done'], checkedField='done', operations=edit),
            view('progress', 'Commitments completed', 'progress', 'focus', ['title', 'done'], checkedField='done'),
            view('rituals', 'Small rituals', 'checklist', 'habits', ['title', 'cadence', 'streak', 'done'], checkedField='done', operations=edit),
        ], [node('progress'), node('week'), node('rituals')]),
        workspace('Reading journal', 'folder', False, '[Demo] A fictional reading shelf and a notebook for ideas worth trying. All titles and authors are invented.', [
            view('shelf', 'On the shelf', 'table', 'books', ['title', 'author', 'format', 'status', 'pages', 'note', 'done'], operations=edit),
            view('mix', 'Reading states', 'chart', 'books', ['title', 'status'], groupBy='status'),
            view('finished', 'Reading completed', 'status', 'books', ['title', 'done'], checkedField='done'),
            view('ideas', 'Put an idea into practice', 'list', 'ideas', ['title', 'topic', 'note', 'done'], operations=edit),
        ], [grid(node('mix'), node('finished')), node('shelf'), node('ideas')]),
        workspace('Launch room', 'projects', True, '[Demo] Coordinate a fictional product launch: delivery, readiness and the reasons behind decisions. These records stay on this computer.', [
            view('readiness', 'Launch readiness', 'progress', 'launch', ['title', 'done'], checkedField='done'),
            view('stages', 'Delivery stages', 'chart', 'launch', ['title', 'status'], groupBy='status'),
            view('deliverables', 'Launch deliverables', 'table', 'launch', ['title', 'owner', 'status', 'due', 'done'], operations=edit),
            view('decisions', 'Decision log', 'list', 'decisions', ['title', 'owner', 'date', 'decision'], operations=edit),
        ], [grid(node('readiness'), node('stages')), node('deliverables'), node('decisions')]),
        workspace('Research hub', 'people', True, '[Demo] Follow a fictional research round from conversations to findings. Participants and organisations are invented; records stay on this computer.', [
            view('interviews', 'Research conversations', 'list', 'interviews', ['participant', 'company', 'segment', 'date', 'insight', 'done'], operations=edit),
            view('themes', 'Themes in the evidence', 'chart', 'findings', ['title', 'theme'], groupBy='theme'),
            view('coverage', 'Interview coverage', 'status', 'interviews', ['participant', 'done'], checkedField='done'),
            view('findings', 'Findings and next steps', 'table', 'findings', ['title', 'theme', 'confidence', 'mentions', 'next_step'], operations=edit),
        ], [grid(node('themes'), node('coverage')), node('findings'), node('interviews')]),
    ]


def seed(personal, team, today, team_id):
    ids, counts = {}, {}
    existing = {c['name']: c for c in personal.items('/collections')}
    for key, (spec, rows) in samples(today).items():
        collection = existing.get(spec['name'])
        if collection is None:
            collection = personal('POST', '/collections', spec)
        elif any(collection[k] != v for k, v in spec.items()):
            raise RuntimeError('Existing sample schema differs; preserving ' + spec['name'])
        cid = ids[key] = collection['id']
        # Stable first-field identity avoids duplicates, including after date changes.
        identity = spec['fields'][0]['key']
        present = {r['values'].get(identity) for r in personal.items(f'/collections/{cid}/records')}
        for values in rows:
            if values[identity] not in present:
                personal('POST', f'/collections/{cid}/records', {'values': values})
        counts[spec['name']] = len(personal.items(f'/collections/{cid}/records'))
    saved = {w['name']: w for w in personal.items('/workspaces')}
    workspaces = []
    for spec in designs(ids, team_id):
        workspace = saved.get(spec['name'])
        if workspace is None:
            workspace = personal('POST', '/workspaces', spec)
        workspaces.append({k: workspace[k] for k in ['id', 'name', 'visibility']})
    members = team('GET', '/members')
    demo_members = []
    for name in ['Sofia Chen', 'Noah Patel', 'Amara Okafor', 'Lucas Martin', 'Elise Laurent', 'Ada Rivera']:
        email = name.lower().replace(' ', '.') + '@demo.example.test'
        member = next((m for m in members if m.get('email') == email), None)
        if member is None:
            # The API returns a token once; discard it, never write or display it.
            member = team('POST', '/members', dict(name=name + ' (Demo)', email=email, role='member'))['member']
        demo_members.append(member)
    # Add the fictional people only to existing, clearly marked demo projects.
    for project in team('GET', '/projects'):
        if not project['name'].startswith('Demo · ') or project.get('archived'):
            continue
        route = f'/projects/{project["id"]}/members'
        present = {m['id'] for m in team('GET', route)}
        for member in demo_members:
            if member['id'] not in present:
                team('PUT', route + '/' + member['id'], {'role': 'member'})
    return dict(workspaces=workspaces, collections=counts, records=sum(counts.values()),
                teamMembers=[dict(id=m['id'], name=m['name']) for m in demo_members])


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--apply', action='store_true')
    parser.add_argument('--data-dir', type=Path, default=Path.home() / 'Library/Application Support/devboard')
    parser.add_argument('--team-key', type=Path, default=Path.home() / 'Library/Application Support/werkbord-team-desktop/access.key')
    parser.add_argument('--date', type=dt.date.fromisoformat, default=dt.date.today())
    args = parser.parse_args()
    if not args.apply:
        parser.error('Pass --apply to add local Demo workspaces, records and team members.')
    args.data_dir = args.data_dir.expanduser().resolve()
    database = args.data_dir / 'devboard.db'
    if not database.is_file():
        parser.error('The existing Individual database was not found.')
    personal = Client(7420, args.data_dir / 'token', '/api/personal/v1')
    team = Client(7431, args.team_key, '/api/team/v1')
    personal('GET', '/capabilities')
    team('GET', '/workspace')
    # Check both services before making a backup or adding data.
    before = dict(workspaces=personal.items('/workspaces'), collections=personal.items('/collections'),
                  teamMembers=team('GET', '/members'))
    backup = args.data_dir / 'backups' / ('before-demo-workspaces-' + dt.datetime.now(dt.timezone.utc).strftime('%Y%m%dT%H%M%S%fZ'))
    backup.mkdir(parents=True, mode=0o700)
    os.chmod(backup, 0o700)
    with sqlite3.connect(database.as_uri() + '?mode=ro', uri=True) as source:
        with sqlite3.connect(backup / 'devboard.db') as destination:
            source.backup(destination)
    os.chmod(backup / 'devboard.db', 0o600)
    before['teamBackup'] = team('POST', '/storage/backup', {})
    (backup / 'before.json').write_text(json.dumps(before, indent=2))
    os.chmod(backup / 'before.json', 0o600)
    result = seed(personal, team, args.date, 'team:main')
    (backup / 'added.json').write_text(json.dumps(result, indent=2))
    os.chmod(backup / 'added.json', 0o600)
    print(json.dumps(dict(result=result, backup=str(backup)), indent=2))


if __name__ == '__main__':
    main()
