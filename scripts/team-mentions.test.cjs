// Run with node --test scripts/team-mentions.test.cjs. The pure part of the Team console's comment box: how the words and the
// people mentioned stay together while someone types (internal/team/console/static/mentions.js).
const { test } = require('node:test');
const assert = require('node:assert/strict');
const M = require('../internal/team/console/static/mentions.js');

const bo = { id: 'tmb_bo', name: 'Bo' };
const person = (id, name, extra = {}) => ({ id, name, ...extra });

// Types text into a box the way a browser reports it: the new value and where the caret is.
function typeAt(box, at, inserted, removed = 0) {
  const next = box.text.slice(0, at) + inserted + box.text.slice(at + removed);
  return { text: next, tokens: M.reconcile(box.text, next, box.tokens, at + inserted.length) };
}

const start = (text, tokens) => ({ text, tokens });
const boxWith = () => {
  // "Hi @Bo , thanks": choosing a person leaves a space after the mention, so what is typed next never runs into it
  const r = M.insert('Hi @', [], { start: 3, query: '' }, 4, bo);
  return typeAt(r, r.caret, ', thanks');
};

test('choosing a person puts their name and their identity in the text and the tokens', () => {
  const r = M.insert('Hi @b', [], { start: 3, query: 'b' }, 5, bo);
  assert.equal(r.text, 'Hi @Bo ');
  assert.deepEqual(r.tokens, [{ start: 3, end: 6, memberId: 'tmb_bo', name: 'Bo' }]);
  assert.equal(r.caret, 7);
  // No second space before a space or a comma, and later tokens move.
  const first = boxWith();
  const second = M.insert(first.text + ' @c', first.tokens, { start: first.text.length + 1, query: 'c' }, first.text.length + 3, person('tmb_cy', 'Cy'));
  assert.deepEqual(second.tokens.map((t) => t.memberId), ['tmb_bo', 'tmb_cy']);
  assert.equal(second.text, 'Hi @Bo , thanks @Cy ');
  const punct = M.insert('x @b, y', [], { start: 2, query: 'b' }, 4, bo);
  assert.equal(punct.text, 'x @Bo, y');
});

test('typing before, after and between mentions keeps them', () => {
  const box = boxWith();
  assert.equal(box.text, 'Hi @Bo , thanks');
  const before = typeAt(box, 0, 'Oh. ');
  assert.deepEqual(before.tokens, [{ start: 7, end: 10, memberId: 'tmb_bo', name: 'Bo' }]);
  assert.equal(before.text.slice(7, 10), '@Bo');
  const after = typeAt(box, box.text.length, '!!');
  assert.deepEqual(after.tokens, box.tokens);
  const adjacentBefore = typeAt(box, 3, 'x'); // "Hi x@Bo": inserted just before the at sign
  assert.equal(adjacentBefore.tokens.length, 1);
  assert.equal(adjacentBefore.text.slice(adjacentBefore.tokens[0].start, adjacentBefore.tokens[0].end), '@Bo');
  const adjacentAfter = typeAt(box, 6, 'x'); // "@Box ,": just after the name
  assert.equal(adjacentAfter.tokens.length, 1);
});

test('touching the words of a mention makes them plain words', () => {
  const box = boxWith();
  for (const [what, edited] of [['a letter typed inside', typeAt(box, 5, 'x')], ['the at sign deleted', typeAt(box, 3, '', 1)], ['a letter deleted', typeAt(box, 5, '', 1)],
    ['the name replaced', typeAt(box, 4, 'Robert', 2)], ['it selected and overtyped', typeAt(box, 3, 'Bob', 3)]]) {
    assert.equal(edited.tokens.length, 0, what);
  }
  // Retyping exactly the same words in its place leaves it a mention (nothing changed).
  const same = typeAt(box, 3, '@Bo', 3);
  assert.equal(same.tokens.length, 1);
});

test('deleting the text around a mention, and pasting over a stretch that holds one', () => {
  const box = boxWith();
  assert.equal(typeAt(box, 0, '', 3).tokens[0].start, 0); // "Hi " removed: the mention moves to the start
  const all = { text: box.text, tokens: M.reconcile(box.text, '', box.tokens, 0) };
  assert.deepEqual(all.tokens, []);
  const pasted = typeAt(box, 0, 'Hello and welcome', box.text.length - 4); // replaces "Hi @Bo, th" (the mention goes) and keeps "anks"
  assert.deepEqual(pasted.tokens, []);
  // Without a caret to go by the same change is still understood.
  const next = 'Hi there @Bo , thanks';
  assert.deepEqual(M.reconcile(box.text, next, box.tokens).map((t) => [t.start, t.end]), [[9, 12]]);
});

test('two mentions in a row of the same character are not confused', () => {
  const r1 = M.insert('@', [], { start: 0, query: '' }, 1, person('a', 'Al'));
  const box = typeAt(r1, r1.caret, '@');
  const two = M.insert(box.text, box.tokens, M.trigger(box.text, box.text.length, box.tokens), box.text.length, person('b', 'Bea'));
  assert.equal(two.text, '@Al @Bea ');
  const removed = typeAt(two, 0, '', 1); // delete the first at sign: Al is words, Bea is still a mention
  assert.deepEqual(removed.tokens.map((t) => t.memberId), ['b']);
  assert.equal(removed.text.slice(removed.tokens[0].start, removed.tokens[0].end), '@Bea');
});

test('what is sent is text and people, in order, with no empty text', () => {
  const box = boxWith();
  assert.deepEqual(M.segments(box.text, box.tokens), [{ type: 'text', text: 'Hi ' }, { type: 'mention', memberId: 'tmb_bo' }, { type: 'text', text: ' , thanks' }]);
  assert.deepEqual(M.segments('@Bo', [{ start: 0, end: 3, memberId: 'tmb_bo', name: 'Bo' }]), [{ type: 'mention', memberId: 'tmb_bo' }]);
  assert.deepEqual(M.segments('plain', []), [{ type: 'text', text: 'plain' }]);
  // A token whose words no longer match is not sent as a mention.
  assert.deepEqual(M.segments('Hi @Bx', [{ start: 3, end: 6, memberId: 'tmb_bo', name: 'Bo' }]), [{ type: 'text', text: 'Hi @Bx' }]);
  // What was written is never read as markup: it is text, whatever it looks like.
  assert.deepEqual(M.segments('<b>x</b>', []), [{ type: 'text', text: '<b>x</b>' }]);
});

test('an existing comment comes back as text with its mentions', () => {
  const box = M.fromSegments([{ type: 'text', text: 'Ask ' }, { type: 'mention', memberId: 'tmb_bo', name: 'Bo' }, { type: 'text', text: ' now' }]);
  assert.equal(box.text, 'Ask @Bo now');
  assert.deepEqual(box.tokens, [{ start: 4, end: 7, memberId: 'tmb_bo', name: 'Bo' }]);
  assert.deepEqual(M.segments(box.text, box.tokens), [{ type: 'text', text: 'Ask ' }, { type: 'mention', memberId: 'tmb_bo' }, { type: 'text', text: ' now' }]);
  assert.deepEqual(M.mentioned([...box.tokens, { start: 9, end: 12, memberId: 'tmb_bo', name: 'Bo' }]), [{ memberId: 'tmb_bo', name: 'Bo' }]);
});

test('the picker opens at an at sign that starts a word, and not inside an address, a mention or a new line', () => {
  const t = (text, caret, tokens = []) => M.trigger(text, caret ?? text.length, tokens);
  assert.deepEqual(t('@'), { start: 0, query: '' });
  assert.deepEqual(t('Hi @bo'), { start: 3, query: 'bo' });
  assert.deepEqual(t('Hi (@bo'), { start: 4, query: 'bo' });
  assert.deepEqual(t('Hi @Ada Lo'), { start: 3, query: 'Ada Lo' });
  assert.deepEqual(t('Hi @bo and more', 6), { start: 3, query: 'bo' });
  assert.equal(t('write to me@example.com'), null);
  assert.equal(t('Hi @ there'), null);
  assert.equal(t('Hi @bo\nnext line'), null);
  assert.equal(t('Hi @bo', 2), null);
  assert.equal(t('Hi'), null);
  assert.equal(t('Hi @Bo', 5, [{ start: 3, end: 6, memberId: 'x', name: 'Bo' }]), null); // inside a mention already made
  assert.equal(t('@' + 'x'.repeat(60)), null);
});

test('people are ranked by how well the query matches, and capped', () => {
  const people = [person('1', 'Zed Ames'), person('2', 'Ada Lovelace', { email: 'ada@x.test' }), person('3', 'Bo Adams'), person('4', 'Cy', { email: 'adam@y.test' }), person('5', 'Dana Ad')];
  assert.deepEqual(M.rank(people, 'ad').map((p) => p.id), ['2', '3', '5', '4']);   // starts with, then a word starts with (by name), then the email
  assert.deepEqual(M.rank(people, 'LOVE').map((p) => p.id), ['2']);                  // case-insensitive, a word of the name
  assert.deepEqual(M.rank(people, 'ada@').map((p) => p.id), ['2']);                  // by email
  assert.deepEqual(M.rank(people, '').map((p) => p.id), ['2', '3', '4', '5', '1']);  // nothing typed: everyone, by name
  assert.deepEqual(M.rank(people, 'nobody'), []);
  const many = Array.from({ length: 30 }, (_, i) => person(String(i), 'Person ' + String(i).padStart(2, '0')));
  assert.equal(M.rank(many, 'pers').length, M.LIMIT);
  assert.deepEqual(M.rank(people, '  ada   lo ').map((p) => p.id), ['2']);          // spacing does not matter
});

test('two people with the same name are told apart, and nobody else is burdened with it', () => {
  const people = [
    person('tmb_a1', 'Alex', { email: 'alex@old.test', projectRole: 'member' }),
    person('tmb_a2', 'alex', { email: 'alex@new.test', projectRole: 'member' }),
    person('tmb_b1', 'Sam', { projectRole: 'reviewer' }), person('tmb_b2', 'Sam', { projectRole: 'reviewer' }),
    person('tmb_c1', 'Robin', { email: 'robin@x.test', projectRole: 'owner' }),
  ];
  const l = M.labels(people);
  assert.equal(l.get('tmb_a1').alike, true);
  assert.notEqual(l.get('tmb_a1').detail, l.get('tmb_a2').detail);
  assert.match(l.get('tmb_a1').detail, /alex@old\.test/);
  // Same name, same role, no email: the end of the ID is what differs.
  assert.notEqual(l.get('tmb_b1').detail, l.get('tmb_b2').detail);
  assert.match(l.get('tmb_b1').detail, /ID …b_b1/);
  assert.match(l.get('tmb_b2').detail, /ID …b_b2/);
  assert.equal(l.get('tmb_c1').alike, false);
  assert.equal(l.get('tmb_c1').detail, 'owner');
  // The same two people are different tokens in a comment, whatever the name says.
  const first = M.insert('@a', [], { start: 0, query: 'a' }, 2, people[0]);
  const second = M.insert(first.text + '@a', first.tokens, { start: first.text.length, query: 'a' }, first.text.length + 2, people[1]);
  assert.deepEqual(M.segments(second.text, second.tokens).filter((s) => s.type === 'mention').map((s) => s.memberId), ['tmb_a1', 'tmb_a2']);
});

test('taking a person out removes every mention of them and nothing else', () => {
  let box = M.insert('see @a', [], { start: 4, query: 'a' }, 6, person('a', 'Al'));
  box = { text: box.text + 'and @b', tokens: box.tokens };
  box = M.insert(box.text, box.tokens, { start: box.text.length - 2, query: 'b' }, box.text.length, person('b', 'Bea'));
  box = M.insert(box.text + '@a', box.tokens, { start: box.text.length, query: 'a' }, box.text.length + 2, person('a', 'Al'));
  assert.equal(box.text, 'see @Al and @Bea @Al ');
  const r = M.removePerson(box.text, box.tokens, 'a');
  assert.equal(r.text, 'see  and @Bea  ');
  assert.deepEqual(r.tokens.map((t) => [t.memberId, r.text.slice(t.start, t.end)]), [['b', '@Bea']]);
});

test('length counts what a person sees, a character at a time', () => {
  assert.equal(M.length('a🙂é'), 3);
  assert.equal(M.length(''), 0);
});
