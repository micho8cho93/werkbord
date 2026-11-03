<script lang="ts">
  import type { Snippet } from 'svelte';
  import BriefCard from '../lib/work/BriefCard.svelte';
  import { ask } from './native';
  import { model } from './model.svelte';

  // The window's catch-up card: the shared one, told which project it is about and how the frame may reach the person's workspaces
  // (the same things the assistant's frame may ask of the app, and nothing else).
  let { children }: { children?: Snippet } = $props();
  const relay = (id: string, method: string, args: unknown[]): Promise<unknown> => {
    if (method === 'AssistantWorkspaces' && args.length === 0) return ask(a => a.AssistantWorkspaces());
    if (method === 'AssistantWorkspaceCall' && args.length === 6 && args.slice(0, 5).every(v => typeof v === 'string')) {
      return ask(a => a.AssistantWorkspaceCall(args[0] as string, args[1] as string, args[2] as string, args[3] as string, args[4] as string, args[5]));
    }
    return ask((a) => a.Relay(id, method, args));
  };
</script>

<BriefCard scope={model.selectedProject ? model.contextLabel : 'all projects'}
  projectContext={model.selectedProject ? { workspace: model.selectedProject.workspace, project: model.selectedProject.project, label: model.contextLabel } : null}
  frame={model.briefFrame} hidden={model.remotePersonal} {relay} prepare={() => { if (model.personalReady) void model.prepareBrief().catch(() => {}); }}>
  {@render children?.()}
</BriefCard>
