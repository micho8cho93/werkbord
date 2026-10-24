import { createLibrary, createParser, defineComponent } from '@openuidev/svelte-lang';
import { z } from 'zod/v4';
import TextBlock from '../assistant/openui/components/TextBlock.svelte';
import HeadingBlock from '../assistant/openui/components/HeadingBlock.svelte';
import CardBlock from '../assistant/openui/components/CardBlock.svelte';
import ContainerBlock from '../assistant/openui/components/ContainerBlock.svelte';
import BindingBlock from './BindingBlock.svelte';
import CollectionBlock from './CollectionBlock.svelte';
import ShortcutBlock from './ShortcutBlock.svelte';
import type { Layout, Workspace } from './schema';

// A separate capability registry. No conversation actions, Query, Mutation, scripts,
// URLs, or assistant state. Presentation uses the existing registered Svelte components.
export const workspaceLibrary = createLibrary({
  components: [
    defineComponent({ description: 'Approved saved workspace component.', name: 'Text', props: z.object({ text: z.string() }), component: TextBlock }),
    defineComponent({ description: 'Approved saved workspace component.', name: 'Heading', props: z.object({ text: z.string() }), component: HeadingBlock }),
    defineComponent({ description: 'Approved saved workspace component.', name: 'Card', props: z.object({ title: z.string(), children: z.array(z.unknown()) }), component: CardBlock }),
    defineComponent({ description: 'Approved saved workspace component.', name: 'Stack', props: z.object({ children: z.array(z.unknown()), direction: z.enum(['vertical', 'grid']), gap: z.literal('large'), columns: z.union([z.literal(2), z.literal(3), z.literal(4)]) }), component: ContainerBlock }),
    defineComponent({ description: 'Approved saved workspace component.', name: 'Collection', props: z.object({ viewId: z.string() }), component: CollectionBlock }),
    defineComponent({ description: 'Approved saved workspace component.', name: 'Shortcut', props: z.object({ target: z.string(), label: z.string() }), component: ShortcutBlock }),
    defineComponent({description:'Declared read-only owner project data.',name:'Binding',props:z.object({bindingId:z.string()}),component:BindingBlock}),
  ], root: 'Stack',
});

/** Compile validated data to OpenUI, quoting every string. Never interpret saved OpenUI source. */
export function workspaceSource(workspace: Workspace): string {
  const lines: string[] = [];
  let next = 0;
  function node(n: Layout): string {
    const id = `n${next++}`;
    const children = (n.children ?? []).map(node);
    const content = `[${children.join(',')}]`;
    const quote = JSON.stringify;
    const expression = n.type === 'text' ? `Text(${quote(n.text)})`
      : n.type === 'heading' ? `Heading(${quote(n.text)})`
      : n.type === 'card' ? `Card(${quote(n.text)},${content})`
      : n.type === 'view' ? `Collection(${quote(n.viewId)})`
      : n.type === 'binding' ? `Binding(${quote(n.bindingId)})`
      : n.type === 'shortcut' ? `Shortcut(${quote(n.target)},${quote(n.text ?? '')})`
      : n.type === 'stack' || n.type === 'grid' ? `Stack(${content},"${n.type === 'grid' ? 'grid' : 'vertical'}","large",${n.columns ?? 2})`
      : (() => { throw new Error('Unsupported saved component.'); })();
    lines.push(`${id} = ${expression}`);
    return id;
  }
  const root = node(workspace.definition.layout);
  const source = `root = Stack([${root}],"vertical","large",2)\n${lines.join('\n')}`;
  const parsed = createParser(workspaceLibrary.toJSONSchema()).parse(source);
  if (!parsed.root || parsed.meta.errors.length || parsed.meta.unresolved.length || parsed.meta.incomplete) throw new Error('This workspace could not be rendered. Correct its saved definition, then reload.');
  return source;
}
