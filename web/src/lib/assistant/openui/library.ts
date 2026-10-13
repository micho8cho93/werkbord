import { createLibrary, createParser, defineComponent, type ParseResult } from '@openuidev/svelte-lang';
import { z } from 'zod/v4';
import CardBlock from './components/CardBlock.svelte';
import BarChartBlock from './components/BarChartBlock.svelte';
import ComparisonChartBlock from './components/ComparisonChartBlock.svelte';
import CheckboxFieldBlock from './components/CheckboxFieldBlock.svelte';
import ContainerBlock from './components/ContainerBlock.svelte';
import ConversationActionBlock from './components/ConversationActionBlock.svelte';
import DataTableBlock from './components/DataTableBlock.svelte';
import DateFieldBlock from './components/DateFieldBlock.svelte';
import DisclosureBlock from './components/DisclosureBlock.svelte';
import FormBlock from './components/FormBlock.svelte';
import GridBlock from './components/GridBlock.svelte';
import HeadingBlock from './components/HeadingBlock.svelte';
import LineChartBlock from './components/LineChartBlock.svelte';
import ListBlock from './components/ListBlock.svelte';
import MetricBlock from './components/MetricBlock.svelte';
import PieChartBlock from './components/PieChartBlock.svelte';
import ProgressBlock from './components/ProgressBlock.svelte';
import StatusSummaryBlock from './components/StatusSummaryBlock.svelte';
import TableBlock from './components/TableBlock.svelte';
import TextBlock from './components/TextBlock.svelte';
import TextFieldBlock from './components/TextFieldBlock.svelte';
import SelectFieldBlock from './components/SelectFieldBlock.svelte';
import TimelineBlock from './components/TimelineBlock.svelte';
import WorkflowBlock from './components/WorkflowBlock.svelte';

// These schemas are the contract for both the prompt and the Svelte renderer. Keep all model-visible components here;
// generated output can only name entries in this registry.
const Text = defineComponent({
  name: 'Text',
  description: 'A paragraph of plain text. Use for explanations and supporting detail.',
  props: z.object({
    text: z.string().max(5000).describe('The paragraph text, kept concise.'),
    tone: z.enum(['normal', 'secondary', 'mono']).optional().describe('Use secondary for quiet supporting text and mono for identifiers or short code-like values.'),
  }),
  component: TextBlock,
});

const snapshotTime = z.string().max(40).describe('The exact observedAt or End timestamp returned by the Werkbord read operation. Label this as a snapshot; it is not live data.');
const countValue = z.number().int().min(0).max(1000000);
const taskState = z.enum(['backlog', 'doing', 'review', 'done']);
const formName = z.string().regex(/^[A-Za-z][A-Za-z0-9_-]{0,39}$/).describe('A stable, unique name for this form in the reply.');
const fieldName = z.string().regex(/^[A-Za-z][A-Za-z0-9_-]{0,47}$/).describe('A stable field name in camelCase, such as plannedStart or projectId.');

const TextField = defineComponent({
  name: 'TextField',
  description: 'A labelled text field inside a Form. Use for a task title, description or other value the person should review or edit before asking the assistant to continue.',
  props: z.object({
    name: fieldName,
    label: z.string().max(100),
    value: z.string().max(500).optional(),
    placeholder: z.string().max(120).optional(),
    required: z.boolean().optional(),
    maxLength: z.number().int().min(1).max(500).optional(),
  }),
  component: TextFieldBlock,
});

const SelectField = defineComponent({
  name: 'SelectField',
  description: 'A native dropdown inside a Form for choosing from explicit, supplied values such as a project or task state. Never invent an identifier or option.',
  props: z.object({
    name: fieldName,
    label: z.string().max(100),
    options: z.array(z.object({ value: z.string().min(1).max(120), label: z.string().max(100) })).min(1).max(16),
    value: z.string().max(120).optional(),
    required: z.boolean().optional(),
  }),
  component: SelectFieldBlock,
});

const DateField = defineComponent({
  name: 'DateField',
  description: 'A native date selector inside a Form. Use ISO dates (YYYY-MM-DD), and leave the value blank when no date was observed or chosen.',
  props: z.object({
    name: fieldName,
    label: z.string().max(100),
    value: z.string().max(10).optional(),
    required: z.boolean().optional(),
  }),
  component: DateFieldBlock,
});

const CheckboxField = defineComponent({
  name: 'CheckboxField',
  description: 'A native checkbox inside a Form for a clear true or false choice.',
  props: z.object({
    name: fieldName,
    label: z.string().max(120),
    checked: z.boolean().optional(),
  }),
  component: CheckboxFieldBlock,
});

const ConversationAction = defineComponent({
  name: 'ConversationAction',
  description: 'A visible button inside a Form. Clicking it explicitly sends its label and that form’s selected values as the user’s next message to the assistant. It cannot call a Werkbord operation or approve a proposal.',
  props: z.object({ label: z.string().trim().min(1).max(160) }),
  component: ConversationActionBlock,
});

const FormContent = z.union([TextField.ref, SelectField.ref, DateField.ref, CheckboxField.ref, ConversationAction.ref]);

const Form = defineComponent({
  name: 'Form',
  description: 'An editable, accessible form shown directly in the conversation. Its fields stay local until a visible ConversationAction is clicked. The click only asks the assistant to continue; any board change still uses Werkbord’s existing proposal and confirmation flow.',
  props: z.object({
    name: formName,
    title: z.string().max(120),
    description: z.string().max(280).optional(),
    children: z.array(FormContent).max(16),
  }),
  component: FormBlock,
});

const Heading = defineComponent({
  name: 'Heading',
  description: 'A section heading inside the generated interface.',
  props: z.object({
    text: z.string().max(160).describe('The heading text.'),
    level: z.union([z.literal(1), z.literal(2), z.literal(3)]).optional().describe('Visual heading level from 1 to 3.'),
  }),
  component: HeadingBlock,
});

const Metric = defineComponent({
  name: 'Metric',
  description: 'A compact labelled measurement with an optional explanation. Include the exact observedAt or End timestamp for this snapshot.',
  props: z.object({
    label: z.string().max(120).describe('What the measurement counts.'),
    value: z.string().max(120).describe('The measured value, formatted as text.'),
    detail: z.string().max(240).optional().describe('A short qualifier or comparison.'),
    asOf: snapshotTime,
  }),
  component: MetricBlock,
});

const Progress = defineComponent({
  name: 'Progress',
  description: 'A measured count against a known total. It computes the visual proportion from the supplied counts and shows no percentage when the total is zero.',
  props: z.object({
    label: z.string().max(120),
    value: countValue,
    total: countValue,
    detail: z.string().max(240).optional(),
    asOf: snapshotTime,
  }),
  component: ProgressBlock,
});

const StatusSummary = defineComponent({
  name: 'StatusSummary',
  description: 'Current task counts by Werkbord board column: Backlog, Doing, Review and Done. Use the exact counts from get_project_metrics and its observedAt timestamp.',
  props: z.object({
    backlog: countValue,
    doing: countValue,
    review: countValue,
    done: countValue,
    asOf: snapshotTime,
  }),
  component: StatusSummaryBlock,
});

const BarChart = defineComponent({
  name: 'BarChart',
  description: 'A horizontal bar chart for one measured value per category. Categories and values must come from a Werkbord read result; pass its exact snapshot timestamp.',
  props: z.object({
    title: z.string().max(160),
    categories: z.array(z.string().max(120)).max(20),
    values: z.array(countValue).max(20),
    unit: z.string().max(20).optional(),
    note: z.string().max(240).optional(),
    asOf: snapshotTime,
  }),
  component: BarChartBlock,
});

const LineChart = defineComponent({
  name: 'LineChart',
  description: 'A dated trend line from recorded values, such as Analytics days.created or days.completed. Keep labels and values aligned and pass the exact report End timestamp.',
  props: z.object({
    title: z.string().max(160),
    labels: z.array(z.string().max(40)).max(91),
    values: z.array(countValue).max(91),
    unit: z.string().max(20).optional(),
    note: z.string().max(240).optional(),
    asOf: snapshotTime,
  }),
  component: LineChartBlock,
});

const PieChart = defineComponent({
  name: 'PieChart',
  description: 'A share-of-total chart for a small set of non-negative counts, such as task states or run outcomes. Zero-valued categories are omitted from the slices but values are not changed.',
  props: z.object({
    title: z.string().max(160),
    categories: z.array(z.string().max(120)).max(12),
    values: z.array(countValue).max(12),
    asOf: snapshotTime,
  }),
  component: PieChartBlock,
});

const ComparisonChart = defineComponent({
  name: 'ComparisonChart',
  description: 'Side-by-side comparison of two measured series across categories, such as provider run attempts and retries. It compares the values supplied and does not infer quality or attribute task completion to an agent.',
  props: z.object({
    title: z.string().max(160),
    categories: z.array(z.string().max(120)).max(16),
    firstLabel: z.string().max(80),
    firstValues: z.array(countValue).max(16),
    secondLabel: z.string().max(80),
    secondValues: z.array(countValue).max(16),
    unit: z.string().max(20).optional(),
    note: z.string().max(240).optional(),
    asOf: snapshotTime,
  }),
  component: ComparisonChartBlock,
});

const DataTable = defineComponent({
  name: 'DataTable',
  description: 'A local sortable and filterable table for task lists, project lists or agent run performance. Sorting and filtering affect only the displayed rows; they do not call Werkbord or change records.',
  props: z.object({
    caption: z.string().max(160),
    columns: z.array(z.string().max(120)).min(1).max(8),
    rows: z.array(z.array(z.string().max(500)).max(8)).max(50),
    asOf: snapshotTime,
    filterable: z.boolean().optional(),
    filters: z.array(z.object({
      column: z.string().max(120),
      label: z.string().max(80),
      options: z.array(z.string().max(100)).max(12),
    })).max(4).optional().describe('Optional local dropdown filters. Each column must exactly match one table column and options must come from its rows.'),
  }),
  component: DataTableBlock,
});

const Timeline = defineComponent({
  name: 'Timeline',
  description: 'A read-only Gantt-style view using recorded planned start and end dates. Include milestone and dependency IDs only when they appear in get_schedule; tasks with incomplete dates remain listed without an invented range.',
  props: z.object({
    title: z.string().max(160),
    items: z.array(z.object({
      id: z.string().max(80).optional(),
      title: z.string().max(200),
      start: z.string().max(40).optional(),
      end: z.string().max(40).optional(),
      state: taskState.optional(),
      milestone: z.boolean().optional(),
      dependsOn: z.array(z.string().max(80)).max(20).optional(),
    })).max(50),
    asOf: snapshotTime,
    note: z.string().max(240).optional(),
  }),
  component: TimelineBlock,
});

const Workflow = defineComponent({
  name: 'Workflow',
  description: 'A read-only task relationship view. It resolves dependency IDs against the supplied task list, marks tasks by task state, and keeps latest run state and recorded blocker reason distinct.',
  props: z.object({
    title: z.string().max(160),
    items: z.array(z.object({
      id: z.string().max(80).optional(),
      title: z.string().max(200),
      state: taskState.optional(),
      runState: z.enum(['starting', 'running', 'waiting_for_user', 'blocked', 'completed', 'failed', 'stopped']).optional(),
      dependsOn: z.array(z.string().max(80)).max(20).optional(),
      blocker: z.string().max(300).optional(),
    })).max(50),
    asOf: snapshotTime,
    note: z.string().max(240).optional(),
  }),
  component: WorkflowBlock,
});

const List = defineComponent({
  name: 'List',
  description: 'A short bulleted list of related plain-text items.',
  props: z.object({
    items: z.array(z.string().max(500)).max(30).describe('Up to 30 concise list items.'),
    title: z.string().optional().describe('Optional list heading.'),
  }),
  component: ListBlock,
});

const Table = defineComponent({
  name: 'Table',
  description: 'A compact snapshot table for comparing structured text values. Include the exact observedAt or End timestamp.',
  props: z.object({
    columns: z.array(z.string().max(120)).min(1).max(6).describe('One to six column headings.'),
    rows: z.array(z.array(z.string().max(500)).max(6)).max(24).describe('Up to 24 rows of cell text in the same order as columns.'),
    caption: z.string().optional().describe('Optional table caption.'),
    asOf: snapshotTime,
  }),
  component: TableBlock,
});

const Leaf = z.union([Text.ref, Heading.ref, Metric.ref, Progress.ref, StatusSummary.ref, List.ref, Table.ref, BarChart.ref, LineChart.ref, PieChart.ref, ComparisonChart.ref, DataTable.ref, Timeline.ref, Workflow.ref]);

const Card = defineComponent({
  name: 'Card',
  description: 'A raised content panel. Use sparingly to group one focused topic.',
  props: z.object({
    title: z.string().max(160).describe('The card title.'),
    children: z.array(Leaf).max(12).optional().describe('Optional text, heading, metric, list, or table content.'),
  }),
  component: CardBlock,
});

const Disclosure = defineComponent({
  name: 'Disclosure',
  description: 'A local, keyboard-operable disclosure for optional details. It does not call tools or change board data.',
  props: z.object({
    title: z.string().max(160).describe('The summary shown while the details are collapsed.'),
    children: z.array(Leaf).max(12).describe('Up to 12 text or structured content items revealed when opened.'),
  }),
  component: DisclosureBlock,
});

const Grid = defineComponent({
  name: 'Grid',
  description: 'A responsive grid for a small set of metrics or focused cards.',
  props: z.object({
    children: z.array(z.union([Metric.ref, Progress.ref, Card.ref])).max(8).describe('Up to 8 metrics, progress indicators or focused cards in the grid.'),
    columns: z.union([z.literal(2), z.literal(3)]).optional().describe('The number of columns on a wide screen; collapses to one on a phone.'),
    gap: z.enum(['small', 'medium', 'large']).optional().describe('Spacing between children.'),
  }),
  component: GridBlock,
});

const Content = z.union([Text.ref, Heading.ref, Metric.ref, Progress.ref, StatusSummary.ref, List.ref, Table.ref, BarChart.ref, LineChart.ref, PieChart.ref, ComparisonChart.ref, DataTable.ref, Timeline.ref, Workflow.ref, Card.ref, Disclosure.ref, Grid.ref, Form.ref]);

const Stack = defineComponent({
  name: 'Stack',
  description: 'The root vertical layout, or a row/grid container for a small set of related components.',
  props: z.object({
    children: z.array(Content).max(30).describe('Up to 30 generated interface components, in display order.'),
    direction: z.enum(['vertical', 'horizontal', 'grid']).optional().describe('Use vertical for sections, horizontal for a short row, or grid for metrics.'),
    gap: z.enum(['small', 'medium', 'large']).optional().describe('Spacing between children.'),
    columns: z.union([z.literal(2), z.literal(3)]).optional().describe('Grid columns when direction is grid.'),
  }),
  component: ContainerBlock,
});

export const openUILibrary = createLibrary({
  components: [Text, Heading, Metric, Progress, StatusSummary, BarChart, LineChart, PieChart, ComparisonChart, DataTable, Timeline, Workflow, List, Table, Card, Disclosure, Grid, Stack, Form, TextField, SelectField, DateField, CheckboxField, ConversationAction],
  root: 'Stack',
});

/** Parse one untrusted response with the same schema that drives this registry's renderer and prompt. */
export function parseOpenUI(source: string): ParseResult {
  return createParser(openUILibrary.toJSONSchema()).parse(source);
}

// OpenUI's inline mode is what lets ordinary prose and a generated interface share a single assistant reply.
export const openUISystemPrompt = openUILibrary.prompt({
  preamble: 'You are generating optional, safe interface content inside Werkbord replies.',
  inlineMode: true,
  toolCalls: false,
  bindings: false,
  additionalRules: [
    'Each message is rendered on its own. When you generate an interface, output the complete interface with its root statement first; do not rely on a previous interface or emit only a patch.',
    'Use only the registered components and their declared arguments. Never output HTML, JavaScript, CSS, event handlers, OpenUI Action() expressions, Query, Mutation, @Run, state bindings, or executable URLs.',
    'Use Form with named TextField, SelectField, CheckboxField and DateField controls when the person should edit a plan or criteria. Keep field names stable and use only observed project or task identifiers and values.',
    'Use ConversationAction only inside a Form. Its visible label is exactly the follow-up the person chooses to send. It only forwards that user-initiated message and the values of that form; it cannot call operations, write board data, confirm changes or use a hidden prompt. Use labels such as “Prepare tasks” or “Review plan” for follow-ups; never label this button “Confirm”, “Confirm all” or “Add tasks”.',
    'Generated components cannot call Werkbord APIs or perform board operations. Ask for board information through the separate Werkbord operation protocol, outside the openui-lang fence.',
    'Creating or changing tasks, dates or schedules must use the existing Werkbord operation protocol. Every proposed change remains uncommitted until the person confirms it in the normal Werkbord confirmation card.',
    'DataTable filters, chart selections and calculations are local presentation state only. Do not claim that they changed board data or fetched a new snapshot.',
    'Retrieve board facts only with the available Werkbord read operations before visualising them. Use exactly their returned counts and records; do not make up missing values, estimates, completion rates, or history.',
    'For current board status use get_project_metrics and its exact Backlog, Doing, Review, Done counts. For history use get_analytics and preserve its range, End snapshot, missingTaskHistory, truncated, and retentionRemoved coverage signals.',
    'get_analytics provider rows describe agent run attempts and outcomes. They do not attribute task completions: one task can have runs from multiple providers. Never chart a provider as having completed a task from those rows.',
    'Visualisations are static snapshots from the operation results, not live data. Include the exact observedAt or End timestamp in asOf on every metric, chart, table, timeline, and workflow.',
    'When a concise text answer is enough, answer in ordinary prose without generating an interface.',
  ],
});
