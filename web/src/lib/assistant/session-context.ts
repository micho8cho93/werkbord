import { getContext, setContext } from 'svelte';
import type { AssistantStore } from './store.svelte';
const SESSION = Symbol('sally-session');
export function provideSession(store: AssistantStore): void { setContext(SESSION, store); }
export function currentSession(): AssistantStore { return getContext<AssistantStore>(SESSION); }
