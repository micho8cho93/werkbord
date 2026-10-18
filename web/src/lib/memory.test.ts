import { describe, expect, it } from 'vitest';
import { memoryLabel, verificationLabel } from './memory';
import type { Memory } from './memory';

const memory: Memory = { id:'m',scope:{ownerId:'local',projectId:'p'},type:'test',authority:'factual_observation',summary:'Tests pass',state:'needs_verification',pinned:true,version:1,source:{type:'handoff',reference:'handoff:r'} };
describe('memory verification labels', () => {
  it('keeps pinned agent claims unverified', () => {
    expect(memoryLabel(memory)).toBe('Suggestion');
    expect(verificationLabel(memory)).toBe('Unverified');
  });
  it('does not treat stale verification as current proof', () => {
    expect(memoryLabel({...memory,verifiedAt:'2026-01-01'})).toBe('Suggestion');
    expect(verificationLabel({...memory,verifiedAt:'2026-01-01'})).toBe('Unverified');
    expect(verificationLabel({...memory,state:'historical',verifiedAt:'2026-01-01'})).toBe('Historical');
  });
  it('distinguishes human confirmation, policies, decisions and preferences', () => {
    const confirmed = {...memory,state:'current',verifiedAt:'2026-01-01',source:{type:'user',reference:'manual:c'}};
    expect(verificationLabel(confirmed)).toBe('Confirmed by you');
    expect(memoryLabel({...confirmed,authority:'approved_policy'})).toBe('Saved policy');
    expect(memoryLabel({...confirmed,authority:'explicit_decision'})).toBe('Decision');
    expect(memoryLabel({...confirmed,authority:'inferred_preference'})).toBe('Preference');
  });
});
