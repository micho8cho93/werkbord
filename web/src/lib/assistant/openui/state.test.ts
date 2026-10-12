import { describe, expect, it } from 'vitest';
import { conversationMessage, persistedOpenUIState, readOpenUIState } from './state';

const form = {
  'sprint-plan': {
    projectId: { value: 'prj_abc123', componentType: 'SelectField' },
    title: { value: 'Review the API', componentType: 'TextField' },
    plannedStart: { value: '2026-10-19', componentType: 'DateField' },
    includeDocs: { value: true, componentType: 'CheckboxField' },
  },
};

describe('OpenUI conversation actions', () => {
  it('turns a visible button click and its scoped values into a user follow-up', () => {
    expect(conversationMessage({
      type: 'continue_conversation',
      params: {},
      humanFriendlyMessage: 'Create tasks from this plan',
      formName: 'sprint-plan',
      formState: form,
    })).toEqual({
      ok: true,
      message: 'Create tasks from this plan\nForm values: {"projectId":"prj_abc123","title":"Review the API","plannedStart":"2026-10-19","includeDocs":true}',
    });
  });

  it('allows an explicit analysis action with no fields', () => {
    expect(conversationMessage({ type: 'continue_conversation', params: {}, humanFriendlyMessage: 'Explain the blocked tasks', formName: 'analysis' }))
      .toEqual({ ok: true, message: 'Explain the blocked tasks' });
  });

  it('accepts a well-formed connected-project identifier and public HTTPS reference', () => {
    expect(conversationMessage({
      type: 'continue_conversation',
      params: {},
      humanFriendlyMessage: 'Create these tasks',
      formName: 'sprint-plan',
      formState: { 'sprint-plan': {
        projectId: { value: 'workspace:team:main/prj_abc123', componentType: 'SelectField' },
        referenceUrl: { value: 'https://docs.example.com/sprint', componentType: 'TextField' },
      } },
    })).toEqual({ ok: true, message: 'Create these tasks\nForm values: {"projectId":"workspace:team:main/prj_abc123","referenceUrl":"https://docs.example.com/sprint"}' });
  });

  it.each([
    ['missing event', null],
    ['URL action', { type: 'open_url', params: { url: 'https://example.com' }, humanFriendlyMessage: 'Open report' }],
    ['unknown operation parameters', { type: 'continue_conversation', params: { operation: 'approve' }, humanFriendlyMessage: 'Approve it' }],
    ['missing visible label', { type: 'continue_conversation', params: {}, humanFriendlyMessage: '' }],
    ['unscoped form values', { type: 'continue_conversation', params: {}, humanFriendlyMessage: 'Continue', formState: form }],
    ['invalid task identifier', { type: 'continue_conversation', params: {}, humanFriendlyMessage: 'Create task', formName: 'sprint-plan', formState: { 'sprint-plan': { projectId: { value: '../../other-board', componentType: 'SelectField' } } } }],
    ['traversal-shaped workspace identifier', { type: 'continue_conversation', params: {}, humanFriendlyMessage: 'Create task', formName: 'sprint-plan', formState: { 'sprint-plan': { projectId: { value: 'workspace:team:main/../private', componentType: 'SelectField' } } } }],
    ['invalid date', { type: 'continue_conversation', params: {}, humanFriendlyMessage: 'Move task', formName: 'sprint-plan', formState: { 'sprint-plan': { plannedStart: { value: '2026-02-30', componentType: 'DateField' } } } }],
    ['unsafe URL value', { type: 'continue_conversation', params: {}, humanFriendlyMessage: 'Use this address', formName: 'references', formState: { references: { sourceUrl: { value: 'http://localhost:3000/private', componentType: 'TextField' } } } }],
    ['private network URL', { type: 'continue_conversation', params: {}, humanFriendlyMessage: 'Use this address', formName: 'references', formState: { references: { sourceUrl: { value: 'https://192.168.1.20/private', componentType: 'TextField' } } } }],
    ['private IPv6 URL', { type: 'continue_conversation', params: {}, humanFriendlyMessage: 'Use this address', formName: 'references', formState: { references: { sourceUrl: { value: 'https://[fd00::1]/private', componentType: 'TextField' } } } }],
    ['unknown form field type', { type: 'continue_conversation', params: {}, humanFriendlyMessage: 'Continue', formName: 'sprint-plan', formState: { 'sprint-plan': { run: { value: 'x', componentType: 'Query' } } } }],
  ])('rejects %s without forwarding it', (_name, event) => {
    expect(conversationMessage(event).ok).toBe(false);
  });

  it('bounds and validates form state before keeping it in the tab', () => {
    expect(persistedOpenUIState(form)).toEqual(form);
    expect(readOpenUIState({ plan: { title: { value: 'x'.repeat(501), componentType: 'TextField' } } }).ok).toBe(false);
    expect(persistedOpenUIState({ plan: { bad: { value: '<script>', componentType: 'TextField', injected: true } } })).toBeUndefined();
    expect(persistedOpenUIState({ plan: {} })).toBeUndefined();
  });
});
