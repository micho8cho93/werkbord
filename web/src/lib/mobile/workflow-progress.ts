import type { AgentOutput, ControllerEvent, Run } from '../types';
import type { DeckSlot } from './layout';

export const stageNames = ['Resolve selected repositories', 'Load repository and project context', 'Analyse source code', 'Verify potential findings', 'Deduplicate and create draft tasks', 'Generate findings report'];
export type StepStatus = 'Pending' | 'Running' | 'Completed' | 'Failed' | 'Cancelled' | 'Waiting for approval';
export interface WorkflowStep { name: string; status: StepStatus; output: string[] }
export const terminal = (run?: Run) => !!run && ['completed', 'failed', 'stopped'].includes(run.state);
export function mergeEvents(old: ControllerEvent[], fresh: ControllerEvent[]): ControllerEvent[] {
  return [...new Map([...old, ...fresh].map(e => [e.seq, e])).values()].sort((a,b) => a.seq-b.seq);
}

/** Rebuild from durable evidence. Missing stage reports never imply completed work. */
export function workflowSteps(run: Run | undefined, events: ControllerEvent[], configuration?: DeckSlot): WorkflowStep[] {
  const names = [...stageNames];
  if (configuration?.button === 'sprint-summary') { names[2] = 'Analyse task, agent and Git activity'; names[3] = 'Verify activity and completion evidence'; }
  if (configuration?.button === 'dependency-check') names[2] = 'Inspect dependency manifests and lockfiles';
  if (configuration?.button === 'security-audit') names[2] = 'Inspect security risks and trust boundaries';
  if (configuration?.button === 'improve-tests') names[2] = 'Identify test coverage gaps';
  if (configuration?.button === 'refactor-codebase') names[2] = 'Analyse maintainability and propose refactors';
  if (configuration && configuration.output !== 'drafts' && !(configuration.button === 'find-bugs' && !configuration.output)) names[4] = 'Deduplicate findings and prepare output';
  const steps: WorkflowStep[] = names.map(name => ({name, status:'Pending', output:[]}));
  steps[0].status = steps[1].status = 'Completed';
  steps[0].output.push('Controller resolved and saved this repository target.');
  steps[1].output.push('Controller captured repository task/run context in the workflow brief.');
  let current = 2;
  let text = '';
  let approval = false;
  for (const ev of mergeEvents([], events)) {
    if (ev.type === 'question.answered' || ev.type === 'question.cancelled') approval = false;
    if (ev.type === 'agent.started') steps[current].status = 'Running';
    if (ev.type === 'agent.output') {
      const out = ev.payload as AgentOutput;
      steps[current].output.push(`${out.stream}: ${out.text}`);
      // Only assistant messages can report investigation stages. Tools and repository text cannot.
      if (out.stream !== 'assistant') continue;
      // Providers may send separate messages without a trailing newline.
      if (out.text.startsWith('WERKBORD_STAGE ') && text && !'WERKBORD_STAGE '.startsWith(text)) text = '';
      text += out.text;
      const lines = text.split('\n');
      text = lines.pop() ?? '';
      if (/^WERKBORD_STAGE [3-6] (running|completed)$/.test(text.trim())) { lines.push(text); text = ''; }
      if (text.length > 4096) text = '';
      for (const line of lines) {
        const match = /^WERKBORD_STAGE ([3-6]) (running|completed)$/.exec(line.trim());
        if (!match) continue;
        const index = Number(match[1])-1;
        if (current !== index && steps[current].status === 'Running') {
          steps[current].status = 'Pending';
          steps[current].output.push('The agent moved to another stage without reporting completion.');
        }
        current = index;
        steps[index].status = match[2] === 'running' ? 'Running' : 'Completed';
      }
    } else if (ev.type === 'agent.failed' || ev.type === 'agent.blocked') {
      steps[current].output.push(JSON.stringify(ev.payload));
    } else if (ev.type === 'agent.question') {
      approval = (ev.payload as {question?: {kind?: string}})?.question?.kind === 'approval';
      steps[current].output.push(JSON.stringify(ev.payload));
    }
  }
  if (run) {
    if (run.state === 'failed') steps[current].status = 'Failed';
    else if (run.state === 'stopped') {
      for (const step of steps) if (step.status !== 'Completed') step.status = 'Cancelled';
    } else if (run.state === 'waiting_for_user' && approval) steps[current].status = 'Waiting for approval';
    else if (run.state === 'completed') {
      for (const step of steps) if (step.status === 'Running') { step.status = 'Pending'; step.output.push('Run completed without a completion report for this stage.'); }
    } else if (!steps.some(s => s.status === 'Running') && steps[current].status === 'Pending' && run.state === 'running') steps[current].status = 'Running';
  }
  return steps;
}
export function workflowStatus(run?: Run): string {
  if (!run) return 'Starting';
  return ({starting:'Starting',running:'Running',waiting_for_user:'Waiting for you',blocked:'Needs attention',completed:'Completed',failed:'Failed',stopped:'Cancelled'})[run.state];
}
export function workflowRecovery(message: string): string {
  if (/auth|sign.?in|credential|permission denied|unauthori[sz]ed/i.test(message)) return 'Check repository and agent sign-in on the runner, then retry.';
  if (/model/i.test(message)) return 'Choose an available model in workflow settings.';
  if (/agent|provider|executable/i.test(message)) return 'Install or select an available agent on the runner in Settings.';
  if (/offline|runner|unreachable/i.test(message)) return 'Reconnect the authorised runner, then retry.';
  if (/repository|worktree|git|directory/i.test(message)) return 'Check repository access and the checkout on the runner, then retry.';
  return 'Inspect the command output and saved results before retrying. Completed results are preserved.';
}
