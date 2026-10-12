import { createLibrary, createParser, defineComponent, type ParseResult } from '@openuidev/svelte-lang';
import { z } from 'zod/v4';
import CardBlock from './components/CardBlock.svelte';
import ContainerBlock from './components/ContainerBlock.svelte';
import DisclosureBlock from './components/DisclosureBlock.svelte';
import GridBlock from './components/GridBlock.svelte';
import HeadingBlock from './components/HeadingBlock.svelte';
import ListBlock from './components/ListBlock.svelte';
import MetricBlock from './components/MetricBlock.svelte';
import TableBlock from './components/TableBlock.svelte';
import TextBlock from './components/TextBlock.svelte';

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
  description: 'A compact labelled measurement with an optional explanation.',
  props: z.object({
    label: z.string().max(120).describe('What the measurement counts.'),
    value: z.string().max(120).describe('The measured value, formatted as text.'),
    detail: z.string().max(240).optional().describe('A short qualifier or comparison.'),
  }),
  component: MetricBlock,
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
  description: 'A compact table for comparing structured text values.',
  props: z.object({
    columns: z.array(z.string().max(120)).min(1).max(6).describe('One to six column headings.'),
    rows: z.array(z.array(z.string().max(500)).max(6)).max(24).describe('Up to 24 rows of cell text in the same order as columns.'),
    caption: z.string().optional().describe('Optional table caption.'),
  }),
  component: TableBlock,
});

const Leaf = z.union([Text.ref, Heading.ref, Metric.ref, List.ref, Table.ref]);

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
    children: z.array(z.union([Metric.ref, Card.ref])).max(8).describe('Up to 8 metrics or cards in the grid.'),
    columns: z.union([z.literal(2), z.literal(3)]).optional().describe('The number of columns on a wide screen; collapses to one on a phone.'),
    gap: z.enum(['small', 'medium', 'large']).optional().describe('Spacing between children.'),
  }),
  component: GridBlock,
});

const Content = z.union([Text.ref, Heading.ref, Metric.ref, List.ref, Table.ref, Card.ref, Disclosure.ref, Grid.ref]);

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
  components: [Text, Heading, Metric, List, Table, Card, Disclosure, Grid, Stack],
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
    'Use only the registered components and their declared arguments. Never output HTML, JavaScript, CSS, event handlers, Action, Query, Mutation, @Run, state bindings, or external URLs as executable UI.',
    'Generated components cannot call Werkbord APIs or perform board operations. Ask for board information through the separate Werkbord operation protocol, outside the openui-lang fence.',
    'Use only facts from the person or Werkbord results. Do not invent counts, dates, status, or other measurements; label estimates and examples clearly.',
    'When a concise text answer is enough, answer in ordinary prose without generating an interface.',
  ],
});
