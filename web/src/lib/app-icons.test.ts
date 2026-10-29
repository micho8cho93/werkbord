import { describe, expect, it } from 'vitest';
import { appIconComponents, appIconSources } from './app-icons';

describe('the app’s own icons', () => {
  const names = Object.keys(appIconComponents) as (keyof typeof appIconComponents)[];
  it('has a source and a component for every name', () => {
    expect(Object.keys(appIconSources).sort()).toEqual([...names].sort());
  });
  it('never draws two meanings with the same icon', () => {
    const sources = Object.values(appIconSources);
    expect(new Set(sources).size).toBe(sources.length);
    const components = Object.values(appIconComponents);
    expect(new Set(components).size).toBe(components.length);
  });
});
