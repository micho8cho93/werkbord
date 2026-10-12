import { describe, expect, it } from 'vitest';
import { openUILibrary, openUISystemPrompt, parseOpenUI } from './library';

describe('Werkbord OpenUI registry', () => {
  it('generates its model prompt and parser schema from the same registered components', () => {
    const names = Object.keys(openUILibrary.components);
    const defs = openUILibrary.toJSONSchema().$defs ?? {};
    for (const name of names) {
      expect(defs).toHaveProperty(name);
      expect(openUISystemPrompt).toContain(name);
    }
    expect(openUISystemPrompt).toContain('## Inline Mode');
    expect(openUISystemPrompt).toContain('Never output HTML, JavaScript, CSS');
    expect(openUISystemPrompt).toContain('root statement first');
  });

  it('parses a nested, structured response with the installed Svelte Lang API', () => {
    const result = parseOpenUI(`root = Stack([heading, metrics, details])
heading = Heading("Sprint status", 1)
metrics = Stack([open, closed], "grid", "small", 2)
open = Metric("Open", "4", "Across two projects")
closed = Metric("Done", "8")
details = Disclosure("See the task list", [tasks])
tasks = List(["Review API", "Update docs"], "Tasks")`);

    expect(result.root?.typeName).toBe('Stack');
    expect(result.meta.errors).toEqual([]);
    expect(result.meta.unresolved).toEqual([]);
  });
});
