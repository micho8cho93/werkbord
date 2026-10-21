import { describe, expect, it } from 'vitest';
import { helpHref, searchHelp } from './help';

describe('bundled Help discovery', () => {
  it('searches guide wording and keyboard shortcuts, without needing a controller search', () => {
    expect(searchHelp('Ctrl+K').map((article) => article.id)).toContain('keyboard');
    expect(searchHelp('awaiting acknowledgment').map((article) => article.id)).toContain('answer');
    expect(searchHelp('assistant conversation from yesterday')).toEqual([]);
    expect(searchHelp('')).toHaveLength(9);
  });

  it('keeps review verdicts distinct from merge and reported tests distinct from verified tests', () => {
    const review = searchHelp('verdict')[0];
    expect(review?.id).toBe('review');
    expect(review?.steps.join(' ')).toMatch(/does not merge/);
    expect(review?.example).toMatch(/reported rather than verified/);
  });

  it('links repository and work projects to views that exist for each kind', () => {
    expect(helpHref({ kind: 'project', repository: 'calendar', work: 'timeline' }, 'repo-id', 'repository')).toBe('#/p/repo-id/calendar');
    expect(helpHref({ kind: 'project', repository: 'calendar', work: 'timeline' }, 'work-id', 'work')).toBe('#/p/work-id/timeline');
    expect(helpHref({ kind: 'settings', section: 'runners' }, 'repo-id', 'repository')).toBe('#/settings?runners');
    expect(helpHref({ kind: 'project', repository: 'runs', work: 'board' }, '', undefined)).toBe('#/projects');
  });
});
