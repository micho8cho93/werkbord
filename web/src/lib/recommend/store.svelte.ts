import { ApiError, api } from '../api';
import { hide, isHidden } from './hidden';
import { isConnectedProject } from './recommend';
import type { RecVerdict, Recommendation, RecommendationSet } from './types';

// Suggested next steps, per project. The rules run on the controller and the answer is shown as it comes: this keeps the last
// answer for a short while so that several screens asking about one project ask once, hides what the person has already
// dismissed, and sends their verdicts back. It starts nothing and decides nothing.

const FRESH_MS = 30_000;

export interface Entry {
  status: 'loading' | 'ready' | 'error';
  set?: RecommendationSet;
  at: number;
  error?: string;
}

class Recommendations {
  entries = $state<Record<string, Entry>>({});
  /** Verdicts being sent, by suggestion id. */
  sending = $state<Record<string, boolean>>({});
  /** The verdict each suggestion has been given in this visit, so that the card can say so. */
  given = $state<Record<string, RecVerdict>>({});
  /** Why the last verdict could not be kept, by suggestion id. */
  failed = $state<Record<string, string>>({});
  private inflight = new Map<string, Promise<void>>();

  entry(projectId: string): Entry | undefined {
    return this.entries[projectId];
  }

  /** The suggestions to show now: what the controller offered, minus what this device has been told to hide. */
  shown(projectId: string): Recommendation[] {
    const set = this.entries[projectId]?.set;
    return (set?.items ?? []).filter((r) => this.given[r.id] !== 'dismissed' && this.given[r.id] !== 'not_useful' && !isHidden(r.id));
  }

  /** Asks for the suggestions, unless a recent answer is held. Several callers at once share one request. */
  load(projectId: string, force = false): Promise<void> {
    if (!projectId) return Promise.resolve();
    const held = this.entries[projectId];
    if (!force && held?.status === 'ready' && Date.now() - held.at < FRESH_MS) return Promise.resolve();
    const running = this.inflight.get(projectId);
    if (running) return running;
    this.entries[projectId] = { status: 'loading', set: held?.set, at: held?.at ?? 0 };
    const p = (async () => {
      try {
        const set = await api.recommendations(projectId);
        this.entries[projectId] = { status: 'ready', set, at: Date.now() };
      } catch (err) {
        // Suggestions are a convenience: a failure is quiet on the screen and says why only where someone asks.
        this.entries[projectId] = { status: 'error', set: held?.set, at: Date.now(), error: err instanceof ApiError ? err.message : 'Suggestions could not be loaded.' };
      } finally {
        this.inflight.delete(projectId);
      }
    })();
    this.inflight.set(projectId, p);
    return p;
  }

  /**
   * Records what the person thought of a suggestion. A dismissal and "not useful" take it off the screen at once and are
   * remembered on this device either way. Where the controller serves memory the verdict is also kept there, as a private
   * preference; a connected workspace's project and a phone keep it on the device only. A verdict that could not be kept in
   * memory is said, and the suggestion stays hidden here regardless.
   */
  async feedback(rec: Recommendation, verdict: RecVerdict): Promise<void> {
    if (this.sending[rec.id]) return;
    this.sending[rec.id] = true;
    delete this.failed[rec.id];
    const set = this.entries[rec.projectId]?.set;
    try {
      if (verdict !== 'useful') hide(rec.id);
      this.given[rec.id] = verdict;
      if (isConnectedProject(rec.projectId) || set?.feedbackStoredIn === 'device') return;
      await api.recommendationFeedback(rec.projectId, { recommendationId: rec.id, intent: rec.intent, variant: rec.variant, verdict });
    } catch (err) {
      this.failed[rec.id] = err instanceof Error ? err.message : 'Your answer could not be saved.';
    } finally {
      delete this.sending[rec.id];
    }
  }
}

export const recommendations = new Recommendations();
