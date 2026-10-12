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
    expect(openUISystemPrompt).toContain('ConversationAction');
    expect(openUISystemPrompt).toContain('Every proposed change remains uncommitted until the person confirms it');
  });

  it('parses a nested, structured response with the installed Svelte Lang API', () => {
    const result = parseOpenUI(`root = Stack([heading, metrics, details])
heading = Heading("Sprint status", 1)
metrics = Stack([open, closed], "grid", "small", 2)
open = Metric("Open", "4", "Across two projects", "2026-03-09T09:00:00Z")
closed = Metric("Done", "8", "", "2026-03-09T09:00:00Z")
details = Disclosure("See the task list", [tasks])
tasks = List(["Review API", "Update docs"], "Tasks")`);

    expect(result.root?.typeName).toBe('Stack');
    expect(result.meta.errors).toEqual([]);
    expect(result.meta.unresolved).toEqual([]);
  });

  it('registers the Werkbord dashboard, chart, table, schedule and workflow components', () => {
    const names = Object.keys(openUILibrary.components);
    for (const name of ['Progress', 'StatusSummary', 'BarChart', 'LineChart', 'PieChart', 'ComparisonChart', 'DataTable', 'Timeline', 'Workflow']) {
      expect(names).toContain(name);
      expect(openUISystemPrompt).toContain(name);
    }
    for (const name of ['Form', 'TextField', 'SelectField', 'CheckboxField', 'DateField', 'ConversationAction']) {
      expect(names).toContain(name);
      expect(openUISystemPrompt).toContain(name);
    }
  });

  it('parses an editable plan form whose visible action only continues the conversation', () => {
    const result = parseOpenUI(`root = Stack([plan])
plan = Form("sprint-plan", "Draft sprint tasks", "Review the values before asking me to prepare changes.", [title, project, start, docs, create])
title = TextField("title", "Task title", "Review the API", "A short title", true, 120)
project = SelectField("projectId", "Project", [{value: "prj_abc123", label: "Werkbord"}], "prj_abc123", true)
start = DateField("plannedStart", "First planned day", "2026-10-19", false)
docs = CheckboxField("includeDocs", "Include documentation tasks", false)
create = ConversationAction("Create tasks from this plan")`);
    expect(result.root?.typeName).toBe('Stack');
    expect(result.meta.errors).toEqual([]);
    expect(result.meta.unresolved).toEqual([]);
  });

  it('parses real-shaped snapshot data without native Query or mutation components', () => {
    const result = parseOpenUI(`root = Stack([status, progress, bars, trend, shares, compare, tasks, timeline, workflow])
status = StatusSummary(2, 1, 1, 4, "2026-03-09T09:00:00Z")
progress = Progress("Done", 4, 8, "Current board state", "2026-03-09T09:00:00Z")
bars = BarChart("Tasks by state", ["Backlog", "Doing", "Done"], [2, 1, 4], "", "Counts from the current board", "2026-03-09T09:00:00Z")
trend = LineChart("Task completions", ["2026-03-03", "2026-03-04"], [1, 2], "", "Historical task state changes", "2026-03-09T09:00:00Z")
shares = PieChart("Run outcomes", ["completed", "failed"], [3, 1], "2026-03-09T09:00:00Z")
compare = ComparisonChart("Provider runs", ["Attempts", "Retries"], "Claude", [5, 1], "Codex", [4, 2], "", "Run outcomes, not task attribution", "2026-03-09T09:00:00Z")
tasks = DataTable("Task list", ["Task", "State"], [["Implement filters", "doing"], ["Review release", "review"]], "2026-03-09T09:00:00Z", true)
timeline = Timeline("Release plan", [{id: "task-1", title: "Implement filters", start: "2026-03-10", end: "2026-03-11", state: "doing", milestone: false, dependsOn: []}], "2026-03-09T09:00:00Z", "Planned dates")
workflow = Workflow("Dependencies", [{id: "task-1", title: "Implement filters", state: "doing", runState: "running", dependsOn: ["task-0"], blocker: "Waiting for API access"}], "2026-03-09T09:00:00Z", "Task and run state are separate")`);

    expect(result.root?.typeName).toBe('Stack');
    expect(result.meta.errors).toEqual([]);
    expect(result.meta.unresolved).toEqual([]);
    expect(result.meta.incomplete).toBe(false);
    expect(openUISystemPrompt).toContain('They do not attribute task completions');
    expect(openUISystemPrompt).toContain('not live data');
    expect(openUISystemPrompt).toContain('Query');
    expect(openUISystemPrompt).toContain('Mutation');
  });
});
