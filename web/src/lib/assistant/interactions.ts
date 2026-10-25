// Source-qualified identities keep proposals and run questions in their original authority context.
import type { Question } from '../types';
import type { AssistantStore } from './store.svelte';
import type { ConfirmItem } from './transcript';
export type Interaction = { key: string; kind: 'question'; question: Question } |
  { key: string; kind: 'proposal'; tabId: string; store: AssistantStore; item: ConfirmItem };
export class InteractionQueue {
  private order: string[] = [];
  reconcile(requests: Interaction[]): Interaction[] {
    const at = (request: Interaction) => Date.parse(request.kind === 'question' ? request.question.askedAt : request.item.createdAt ?? '') || 0;
    const current = new Map([...requests].sort((a, b) => at(a) - at(b)).map(r => [r.key, r]));
    this.order = this.order.filter(key => current.has(key));
    for (const key of current.keys()) if (!this.order.includes(key)) this.order.push(key);
    return this.order.map(key => current.get(key)!);
  }
}
