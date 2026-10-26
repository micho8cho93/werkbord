#!/usr/bin/env python3
"""Populate the local generated-workspace library and Team directory with Demo data.

Uses only the authenticated loopback APIs for writes. Requires --apply, snapshots
Individual and asks Team to back up before any sample data is added. No agents,
invitations, schedules or remote connections are created. Safe to rerun: existing
named workspaces/collections/members are preserved and missing sample rows added.

The workspaces use the productivity views: documents, pages, boards, calendars,
snippets, file catalogues and shortcuts. Pass --replace to bring workspaces made by an
earlier version of this script up to date (same workspace, same icon and places; the
earlier definition stays in its history and can be restored). Their old sample
collections are kept in the Data library, untouched.
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
            items.extend(page['items'] or [])
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
    body = field('body', 'Text', 'longtext')
    parent = field('parent', 'Parent page')
    done = field('done', 'Completed', 'boolean')
    owner = field('owner', 'Owner')
    people = ['Sofia Chen', 'Noah Patel', 'Amara Okafor', 'Lucas Martin', 'Elise Laurent', 'Ada Rivera']
    c = {}

    # ---- Focus planner -----------------------------------------------------------------------------------------
    c['focus_board'] = collection('Focus board', [title,
        field('status', 'Status', 'select', ['To do', 'Doing', 'Done']),
        field('area', 'Area', 'select', ['Build', 'Learn', 'Life']),
        field('due', 'Planned date', 'date'), field('minutes', 'Minutes', 'number')], [
        dict(title=t, status=(['Done'] * 4 + ['Doing'] * 4 + ['To do'] * 6)[i], area=['Build', 'Learn', 'Life'][i % 3], due=date(i - 4), minutes=[25, 45, 60][i % 3])
        for i, t in enumerate(['Sketch the settings flow', 'Practise a Spanish conversation', 'Plan the coastal weekend',
            'Clear the feedback inbox', 'Read the keyboard access notes', 'Book the bicycle service',
            'Prototype the command palette', 'Write a learning retrospective', 'Plan meals for next week',
            'Review the search interaction', 'Try a new testing technique', 'Organise the studio shelf',
            'Draft the quarterly goals', 'Call the landlord'])])
    c['habits'] = collection('Weekly rituals', [title,
        field('cadence', 'Cadence', 'select', ['Daily', 'Weekly']),
        field('streak', 'Streak in weeks', 'number'), done], [
        dict(title=t, cadence='Daily' if i < 3 else 'Weekly', streak=[4, 7, 2, 8, 3, 6, 1][i], done=i < 3)
        for i, t in enumerate(['Protect a morning focus hour', 'Walk without a screen',
            'Capture one learning note', 'Review the coming week', 'Back up personal files',
            'Tidy the workbench', 'Make room for a creative experiment'])])
    c['weekly_notes'] = collection('Weekly notes', [title, body,
        field('tags', 'Tags', 'multiselect', ['Review', 'Idea', 'Decision'])], [
        dict(title='Weekly review', tags=['Review'], body=f"""# Weekly review

*[Demo] A page to write in. Everything here is saved as you type.*

## What moved
- [x] Settings flow sketched
- [x] Spanish conversation practised
- [ ] Feedback inbox cleared

## What got in the way
A **long meeting** on Tuesday took the whole afternoon. Next week, protect one morning with no calls.

## Next week
1. Prototype the command palette
2. Plan meals
3. Book the bicycle service

> Fewer commitments leave room to think.
"""),
        dict(title='Ideas to try', tags=['Idea'], body="""# Ideas to try

- A **ten minute** shutdown ritual at the end of each day
- Leave one afternoon a week with *nothing* planned
- Write the reason for a decision next to the decision

See also [[Weekly review]].
"""),
        dict(title='Decisions', tags=['Decision'], body="""# Decisions

Written as plain lists, because lists are easiest to scan:

- **Mornings are for building.** Calls move to after lunch.
- **One inbox.** Feedback goes to a single list and is cleared on Fridays.
- **Say no early.** A late no costs more than an early one.
""")])

    # ---- Reading journal ---------------------------------------------------------------------------------------
    c['books'] = collection('Reading shelf', [title, field('author', 'Author'),
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
    pages = field('tags', 'Tags', 'multiselect', ['Design', 'Engineering', 'Practice'])
    c['reading_pages'] = collection('Reading notes (pages)', [title, body, parent, pages], [
        dict(title='Reading hub', tags=['Practice'], body="""# Reading hub

*[Demo] Notes on what I read, one page for each idea. All titles and authors are invented.*

Start with [[Design books]] or [[Engineering books]]. Ideas worth keeping go to [[Ideas to try (reading)]].

- [ ] Finish **Quiet Systems**
- [ ] Write up *Local by Design*
"""),
        dict(title='Design books', parent='Reading hub', tags=['Design'], body="""# Design books

Three books that changed how I draw the first screen:

1. [[The Shape of Everyday Tools]]
2. [[Designing for Attention]]
3. [[Readable Interfaces]]
"""),
        dict(title='Engineering books', parent='Reading hub', tags=['Engineering'], body="""# Engineering books

- [[The Patient Programmer]]: small changes make review easier
- *Quiet Systems*: a good default removes a decision
"""),
        dict(title='The Shape of Everyday Tools', parent='Design books', tags=['Design'], body="""# The Shape of Everyday Tools

*Mira Vale · finished*

## The idea
Make the **frequent action** easy to find, and the rare one safe to find later.

## What I will try
- [x] Move the most used button to the first position
- [ ] Remove one rarely used control from the toolbar

Related: [[Designing for Attention]].
"""),
        dict(title='Designing for Attention', parent='Design books', tags=['Design'], body="""# Designing for Attention

*Samir Reed · reading*

Put the next useful step **beside the evidence** for it. A button far from the reason to press it is a guess.
"""),
        dict(title='Readable Interfaces', parent='Design books', tags=['Design'], body="""# Readable Interfaces

Labels should survive a narrow screen. Read every primary route at *phone width* before calling it done.
"""),
        dict(title='The Patient Programmer', parent='Engineering books', tags=['Engineering'], body="""# The Patient Programmer

*Lena Moss*

Small changes make review easier. Split the next refactor at a visible behaviour boundary.

```sh
git log --oneline -n 20
```
"""),
        dict(title='Ideas to try (reading)', parent='Reading hub', tags=['Practice'], body="""# Ideas to try

- Keep an observation log: three concrete notes after each work session
- Name the **recovery action** in every error message
- Leave a written handoff with the decision and one open question

Back to [[Reading hub]].
""")])

    # ---- Launch room -------------------------------------------------------------------------------------------
    c['launch'] = collection('Launch deliverables', [title, owner,
        field('status', 'Stage', 'select', ['Planned', 'In progress', 'Review', 'Ready']),
        field('due', 'Due date', 'date'), done], [
        dict(title=t, owner=people[i % 6] + ' (Demo)', status=['Ready', 'Review', 'In progress', 'Planned'][i % 4],
             due=date(i - 2), done=i % 4 == 0)
        for i, t in enumerate(['Confirm the launch story', 'Review the pricing explanation',
            'Prepare the product walkthrough', 'Write the announcement draft', 'Check the support playbook',
            'Validate the migration guide', 'Record a short demo', 'Review accessibility fixes',
            'Prepare customer questions', 'Run the release rehearsal', 'Collect the final approvals',
            'Plan the first-week retrospective'])])
    c['decisions'] = collection('Launch decisions', [title, owner,
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
    c['launch_docs'] = collection('Launch documents', [title, body,
        field('tags', 'Tags', 'multiselect', ['Brief', 'Copy', 'Support'])], [
        dict(title='Launch brief', tags=['Brief'], body="""# Launch brief: Lantern

*[Demo] A fictional product and a fictional launch, written for sample data.*

## Goal
Make the first week of **Lantern** calm: a clear story, a working walkthrough and fast answers.

## The story in one line
> Fewer tools. Clearer handoffs.

## What must be true by launch day
- [x] The launch story is agreed
- [x] The walkthrough runs from a clean install
- [ ] Support has the first-response guide
- [ ] The migration guide is validated

## Owners
Sofia leads the story, Noah the walkthrough and Amara support.

## Open questions
1. Do we announce on a Monday or a Tuesday?
2. Who answers questions on the first evening?
"""),
        dict(title='Announcement draft', tags=['Copy'], body="""# Lantern is here

Lantern keeps the next step of your work in view: **one calm board**, **one place for notes**.

## Try it this week
1. Add the three things that matter most
2. Write one note about why
3. Come back on Friday and look at what moved

Questions? Write to hello@demo.example.test.
"""),
        dict(title='First-response guide', tags=['Support'], body="""# First-response guide

Use these answers for the first evening. Replace anything that is not true yet.

- **Where do I start?** Open the board and add one task.
- **Can I import my notes?** Yes: paste them into a page; headings and lists are kept.
- **Is my data private?** It stays on your computer.

Escalate anything about *data loss* to Sofia at once.
""")])
    c['launch_assets'] = collection('Launch assets', [title,
        field('kind', 'Kind', 'select', ['One-pager', 'Picture', 'Copy']),
        field('tags', 'Tags', 'multiselect', ['Brochure', 'Brand', 'Draft']), field('notes', 'Notes', 'longtext')], [
        dict(title='Lantern launch one-pager', kind='One-pager', tags=['Brochure', 'Draft'], notes='Sample PDF: scripts/demo-assets/lantern-launch-one-pager.pdf. Attach it to read it here.'),
        dict(title='Lantern colours', kind='Picture', tags=['Brand'], notes='Sample picture: scripts/demo-assets/lantern-colours.png.'),
        dict(title='Lantern announcement', kind='Copy', tags=['Brochure'], notes='Sample Markdown: scripts/demo-assets/lantern-announcement.md.')])

    # ---- Research hub ------------------------------------------------------------------------------------------
    c['interviews'] = collection('Research interviews', [
        field('participant', 'Participant', required=True), field('company', 'Fictional organisation'),
        field('segment', 'Segment', 'select', ['Independent', 'Small team', 'Studio']),
        field('date', 'Interview date', 'date'), field('insight', 'Observation'), done], [
        dict(participant=p + ' (Demo)', company=cmp, segment=['Independent', 'Small team', 'Studio'][i % 3],
             date=date(i - 7), insight='[Demo] ' + n, done=i < 8)
        for i, (p, cmp, n) in enumerate([
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
    c['findings'] = collection('Research findings', [title,
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
    topics = field('tags', 'Tags', 'multiselect', ['Interview', 'Synthesis', 'Method'])
    c['research_pages'] = collection('Research notes (pages)', [title, body, parent, topics], [
        dict(title='Research hub', tags=['Method'], body="""# Research hub

*[Demo] A fictional research round. Participants and organisations are invented; everything stays on this computer.*

- [[Interview notes]]: one page for each conversation
- [[Synthesis]]: what the conversations add up to
- [[Method]]: how we ran the round

- [x] Recruit eight participants
- [x] Run the first eight conversations
- [ ] Write up the synthesis
"""),
        dict(title='Interview notes', parent='Research hub', tags=['Interview'], body="""# Interview notes

Each conversation has its own page. Add a sub-page with the **+** beside this page in the tree.

- [[Alex Rowan (Demo)]]
- [[Morgan Lake (Demo)]]
"""),
        dict(title='Alex Rowan (Demo)', parent='Interview notes', tags=['Interview'], body="""# Alex Rowan (Demo)

*Cedar Workshop · independent*

## What they said
- Wants **one clear view** of the next action
- Closes the laptop when a screen asks for three decisions

## Quote
> I do not need more features. I need to know what to do next.

Feeds [[Synthesis]].
"""),
        dict(title='Morgan Lake (Demo)', parent='Interview notes', tags=['Interview'], body="""# Morgan Lake (Demo)

*Northline Studio · studio*

Needs a **durable handoff** after review. The decision and the reason must travel together.

Feeds [[Synthesis]].
"""),
        dict(title='Synthesis', parent='Research hub', tags=['Synthesis'], body="""# Synthesis

## Themes
1. **The next action should be obvious** (strong): [[Alex Rowan (Demo)]]
2. **A handoff needs its decision history** (supported): [[Morgan Lake (Demo)]]
3. **Labels matter more on narrow screens** (emerging)

## What we will do
- [ ] Test the task entry point with three participants
- [ ] Try a decision-and-question handoff template
"""),
        dict(title='Method', parent='Research hub', tags=['Method'], body="""# Method

Thirty-minute conversations, one question at a time. The script is in the snippet library of this workspace.

- Ask about the **last real example**, not what they would do
- Write observations first and interpretation later
""")])
    c['questions'] = collection('Interview questions', [title,
        field('topic', 'Topic', 'select', ['Warm-up', 'Workflow', 'Wrap-up']), field('text', 'Question', 'longtext')], [
        dict(title='Open', topic='Warm-up', text='Tell me about the last time you planned a week of work.\nWhat did you open first?'),
        dict(title='Last real example', topic='Workflow', text='Think of the last handoff you made.\nWalk me through it: what did you write down, and what did you leave out?'),
        dict(title='The unclear moment', topic='Workflow', text='When did you last not know what to do next?\nWhat did you do instead?'),
        dict(title='Where it lives', topic='Workflow', text='Where do decisions get written down today?\nHow do you find one a month later?'),
        dict(title='Close', topic='Wrap-up', text='What is the one thing you wish had been easier this week?\nIs there anyone else I should talk to?'),
    ])

    # ---- Developer cockpit -------------------------------------------------------------------------------------
    langs = field('lang', 'Language', 'select', ['Shell', 'Git', 'SQL', 'Go'])
    c['dev_snippets'] = collection('Developer snippets', [title, langs, field('text', 'Snippet', 'longtext')], [
        dict(title='Run every check', lang='Shell', text='make check'),
        dict(title='Last twenty commits', lang='Git', text='git log --oneline -n 20'),
        dict(title='Undo the last commit, keep the changes', lang='Git', text='git reset --soft HEAD~1'),
        dict(title='Branches by recent work', lang='Git', text="git for-each-ref --sort=-committerdate refs/heads/ \\\n  --format='%(committerdate:short) %(refname:short)'"),
        dict(title='Find what is listening on a port', lang='Shell', text='lsof -nP -iTCP:7420 -sTCP:LISTEN'),
        dict(title='Watch a log, only the errors', lang='Shell', text='tail -f app.log | grep --line-buffered -i error'),
        dict(title='Slowest queries', lang='SQL', text='SELECT query, calls, mean_exec_time\nFROM pg_stat_statements\nORDER BY mean_exec_time DESC\nLIMIT 10;'),
        dict(title='Table-driven test', lang='Go', text='func TestSum(t *testing.T) {\n\tfor _, c := range []struct{ in []int; want int }{\n\t\t{nil, 0}, {[]int{1, 2}, 3},\n\t} {\n\t\tif got := sum(c.in); got != c.want {\n\t\t\tt.Errorf("sum(%v) = %d, want %d", c.in, got, c.want)\n\t\t}\n\t}\n}'),
    ])
    c['runbooks'] = collection('Runbooks (pages)', [title, body, parent, field('tags', 'Tags', 'multiselect', ['Release', 'Incident', 'Review'])], [
        dict(title='Runbooks', tags=['Release'], body="""# Runbooks

*[Demo] Checklists to follow, not to remember.*

- [[Release runbook]]
- [[Incident checklist]]
- [[Review checklist]]
"""),
        dict(title='Release runbook', parent='Runbooks', tags=['Release'], body="""# Release runbook

## Before
- [ ] `make check` passes
- [ ] The version file is bumped
- [ ] The notes say what changed, in plain words

## Ship
1. Tag the commit
2. Verify the tag
3. Publish the notes

## After
- [ ] Watch the first hour of reports
- [ ] Write down anything surprising in [[Review checklist]]
"""),
        dict(title='Incident checklist', parent='Runbooks', tags=['Incident'], body="""# Incident checklist

1. **Say so.** Post what is known and what is not
2. **Stabilise** before you diagnose
3. **Write the timeline** as it happens

> The goal is a calm record, not a clever fix.
"""),
        dict(title='Review checklist', parent='Runbooks', tags=['Review'], body="""# Review checklist

- [ ] The change does what its title says
- [ ] A failing test existed first
- [ ] Names say what things are for
- [ ] Nothing was added that nothing uses
"""),
    ])
    c['sprint'] = collection('Sprint', [title,
        field('stage', 'Stage', 'select', ['Backlog', 'In progress', 'In review', 'Shipped']),
        field('area', 'Area', 'select', ['Editor', 'Boards', 'Platform']),
        field('due', 'Due', 'date'), field('link', 'Link', 'url')], [
        dict(title='Autosave failure message', stage='Shipped', area='Editor', due=date(-6), link='https://example.com/demo/1'),
        dict(title='Drag cards between columns', stage='In review', area='Boards', due=date(-1), link='https://example.com/demo/2'),
        dict(title='Calendar week starts on Monday', stage='In progress', area='Boards', due=date(2)),
        dict(title='Pages: move under another page', stage='In progress', area='Editor', due=date(3)),
        dict(title='Export a document as Markdown', stage='Backlog', area='Editor', due=date(9)),
        dict(title='Backup before upgrade', stage='Backlog', area='Platform', due=date(12), link='https://example.com/demo/6'),
    ])

    # ---- Library collections nobody's workspace uses ----------------------------------------------------------
    c['recipes'] = collection('Kitchen experiments', [
        field('dish', 'Dish', required=True), field('category', 'Category', 'select', ['Lunch', 'Dinner', 'Baking']),
        field('minutes', 'Minutes', 'number'), field('favourite', 'Favourite', 'boolean')], [
        dict(dish=t, category=['Lunch', 'Dinner', 'Baking'][i % 3], minutes=20 + i * 5, favourite=i % 3 == 0)
        for i, t in enumerate(['Lemon chickpea bowl', 'Roasted pepper pasta', 'Rosemary focaccia',
            'Herbed lentil salad', 'Mushroom barley pot', 'Orange almond cake', 'Crisp potato wraps', 'Aubergine tray bake'])])
    c['risks'] = collection('Team risk register', [title, owner,
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
    return c


def designs(ids, team_id):
    def view(key, name, kind, collection, fields, **extra):
        return dict(id=key, name=name, kind=kind, collectionId=ids[collection], fields=fields, **extra)

    def node(key):
        return dict(type='view', viewId=key)

    def grid(*children, columns=2):
        return dict(type='grid', columns=columns, children=list(children))

    def go(target, label=None):
        return dict(type='shortcut', target=target, **({'text': label} if label else {}))

    def workspace(name, team, intro, views, body, individual=None, icon='auto'):
        return dict(name='Demo · ' + name, icon=icon, uiSchemaVersion=1,
                    visibility=dict(universal=False, individual=(not team) if individual is None else individual, teamWorkspaceIds=[team_id] if team else []),
                    collectionIds=list(dict.fromkeys(v['collectionId'] for v in views)), readBindings=[],
                    definition=dict(views=views, layout=dict(type='stack', children=[dict(type='text', text=intro), *body])))

    edit = ['create', 'update', 'delete']
    return [
        workspace('Focus planner', False, '[Demo] A week with room to build, learn and live. Move commitments across the board, see them on the calendar, write the weekly review, and protect small rituals.', [
            view('board', 'This week', 'board', 'focus_board', ['title', 'area', 'due'], groupBy='status', operations=edit),
            view('mix', 'Where the week is', 'chart', 'focus_board', ['title', 'status'], groupBy='status'),
            view('calendar', 'On the calendar', 'calendar', 'focus_board', ['title'], dateField='due', operations=edit),
            view('notes', 'Weekly notes', 'document', 'weekly_notes', ['title', 'tags'], bodyField='body', operations=edit),
            view('rituals', 'Small rituals', 'checklist', 'habits', ['title', 'cadence', 'streak', 'done'], checkedField='done', operations=edit),
        ], [node('board'), grid(node('mix'), node('rituals')), node('calendar'), node('notes')]),
        workspace('Reading journal', False, '[Demo] A notebook of linked pages for what I read, beside the shelf. All titles and authors are invented.', [
            view('pages', 'Reading notes', 'pages', 'reading_pages', ['title', 'tags'], bodyField='body', parentField='parent', operations=edit),
            view('shelf', 'On the shelf', 'table', 'books', ['title', 'author', 'format', 'status', 'pages', 'note', 'done'], operations=edit),
            view('mix', 'Reading states', 'chart', 'books', ['title', 'status'], groupBy='status'),
            view('finished', 'Reading completed', 'status', 'books', ['title', 'done'], checkedField='done'),
        ], [node('pages'), grid(node('mix'), node('finished')), node('shelf')]),
        workspace('Launch room', True, '[Demo] Coordinate a fictional product launch: documents to write, a board to move, dates to meet and material to read. These records stay on this computer.', [
            view('docs', 'Launch documents', 'document', 'launch_docs', ['title', 'tags'], bodyField='body', operations=edit),
            view('board', 'Deliverables', 'board', 'launch', ['title', 'owner', 'due'], groupBy='status', operations=edit),
            view('calendar', 'Due dates', 'calendar', 'launch', ['title'], dateField='due', operations=edit),
            view('assets', 'Launch material', 'files', 'launch_assets', ['title', 'kind', 'tags'], bodyField='notes', operations=edit),
            view('decisions', 'Decision log', 'list', 'decisions', ['title', 'owner', 'date', 'decision'], operations=edit),
        ], [node('docs'), node('board'), node('calendar'), node('assets'), node('decisions')]),
        workspace('Research hub', True, '[Demo] Follow a fictional research round from conversations to findings: linked notes, a question bank and the evidence. Participants and organisations are invented; records stay on this computer.', [
            view('notes', 'Research notes', 'pages', 'research_pages', ['title', 'tags'], bodyField='body', parentField='parent', operations=edit),
            view('questions', 'Interview questions', 'snippets', 'questions', ['title', 'topic'], bodyField='text', groupBy='topic', operations=edit),
            view('themes', 'Themes in the evidence', 'chart', 'findings', ['title', 'theme'], groupBy='theme'),
            view('coverage', 'Interview coverage', 'status', 'interviews', ['participant', 'done'], checkedField='done'),
            view('findings', 'Findings and next steps', 'table', 'findings', ['title', 'theme', 'confidence', 'mentions', 'next_step'], operations=edit),
        ], [node('notes'), node('questions'), grid(node('themes'), node('coverage')), node('findings')]),
        workspace('Developer cockpit', False, '[Demo] Everything a day of building reaches for: the places in the app, commands to copy, runbooks to follow and a sprint to move.', [
            view('sprint', 'Sprint', 'board', 'sprint', ['title', 'area', 'due', 'link'], groupBy='stage', operations=edit),
            view('snippets', 'Commands and snippets', 'snippets', 'dev_snippets', ['title', 'lang'], bodyField='text', groupBy='lang', operations=edit),
            view('runbooks', 'Runbooks', 'pages', 'runbooks', ['title', 'tags'], bodyField='body', parentField='parent', operations=edit),
        ], [grid(go('projects'), go('analytics'), go('calendar'), go('assistant', 'Ask Salie'), columns=4), node('sprint'), node('snippets'), node('runbooks')], individual=True, icon='lucide-terminal'),
    ]


def seed(personal, team, today, team_id, replace=False):
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
        parent_key = 'parent' if any(f['key'] == 'parent' for f in spec['fields']) else None
        records = personal.items(f'/collections/{cid}/records')
        by_identity = {r['values'].get(identity): r['id'] for r in records}
        for values in rows:
            if values[identity] in by_identity:
                continue
            values = dict(values)
            # A page names its parent by title; the record keeps the parent's ID.
            if parent_key and values.get(parent_key):
                values[parent_key] = by_identity[values[parent_key]]
            by_identity[values[identity]] = personal('POST', f'/collections/{cid}/records', {'values': values})['id']
        counts[spec['name']] = len(personal.items(f'/collections/{cid}/records'))
    saved = {w['name']: w for w in personal.items('/workspaces')}
    workspaces = []
    for spec in designs(ids, team_id):
        workspace = saved.get(spec['name'])
        action = 'kept'
        if workspace is None:
            try:
                workspace = personal('POST', '/workspaces', spec)
            except RuntimeError:
                # Another workspace already has the icon asked for: let the controller choose a free one.
                workspace = personal('POST', '/workspaces', dict(spec, icon='auto'))
            action = 'created'
        elif replace and workspace['definition'] != spec['definition']:
            # Same workspace, same icon and the places it already appears in; the earlier definition stays in its history.
            replacement = dict(spec, icon=workspace['icon'], visibility=workspace['visibility'])
            workspace = personal('PUT', '/workspaces/' + workspace['id'], dict(revision=workspace['revision'], workspace=replacement))
            action = 'replaced'
        workspaces.append(dict(id=workspace['id'], name=workspace['name'], visibility=workspace['visibility'], action=action))
    return dict(workspaces=workspaces, collections=counts, records=sum(counts.values()))


def seed_team(team, team_id):
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
    return [dict(id=m['id'], name=m['name']) for m in demo_members]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--apply', action='store_true')
    parser.add_argument('--replace', action='store_true', help='bring Demo workspaces from an earlier version up to date')
    parser.add_argument('--data-dir', type=Path, default=Path.home() / 'Library/Application Support/werkbord')
    parser.add_argument('--db-name', default='werkbord.db')
    parser.add_argument('--port', type=int, default=7420)
    parser.add_argument('--team-key', type=Path, default=Path.home() / 'Library/Application Support/werkbord-team-desktop/access.key')
    parser.add_argument('--skip-team', action='store_true', help='add only the Individual workspaces and collections')
    parser.add_argument('--date', type=dt.date.fromisoformat, default=dt.date.today())
    args = parser.parse_args()
    if not args.apply:
        parser.error('Pass --apply to add local Demo workspaces, records and team members.')
    args.data_dir = args.data_dir.expanduser().resolve()
    database = args.data_dir / args.db_name
    if not database.is_file():
        parser.error('The existing Individual database was not found.')
    personal = Client(args.port, args.data_dir / 'token', '/api/personal/v1')
    team = None if args.skip_team else Client(7431, args.team_key, '/api/team/v1')
    personal('GET', '/capabilities')
    if team:
        team('GET', '/workspace')
    # Check both services before making a backup or adding data.
    before = dict(workspaces=personal.items('/workspaces'), collections=personal.items('/collections'),
                  teamMembers=team('GET', '/members') if team else [])
    backup = args.data_dir / 'backups' / ('before-demo-workspaces-' + dt.datetime.now(dt.timezone.utc).strftime('%Y%m%dT%H%M%S%fZ'))
    backup.mkdir(parents=True, mode=0o700)
    os.chmod(backup, 0o700)
    with sqlite3.connect(database.as_uri() + '?mode=ro', uri=True) as source:
        with sqlite3.connect(backup / 'werkbord.db') as destination:
            source.backup(destination)
    os.chmod(backup / 'werkbord.db', 0o600)
    if team:
        before['teamBackup'] = team('POST', '/storage/backup', {})
    (backup / 'before.json').write_text(json.dumps(before, indent=2))
    os.chmod(backup / 'before.json', 0o600)
    result = seed(personal, team, args.date, 'team:main', args.replace)
    if team:
        result['teamMembers'] = seed_team(team, 'team:main')
    (backup / 'added.json').write_text(json.dumps(result, indent=2))
    os.chmod(backup / 'added.json', 0o600)
    print(json.dumps(dict(result=result, backup=str(backup)), indent=2))
    assets = Path(__file__).parent / 'demo-assets'
    print(f'\nTo read the sample files in Launch room → Launch material, open an entry and attach a file from {assets}.')


if __name__ == '__main__':
    main()
