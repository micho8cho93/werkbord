import { beforeEach, describe, expect, it, vi } from 'vitest';
const fake = vi.hoisted(() => ({ listTaskRuns: vi.fn(), startRun: vi.fn(), sendInput: vi.fn(), waiting: vi.fn(), linkWaiting: vi.fn(), githubAddRepo: vi.fn() }));
vi.mock('../api', () => ({ api: fake }));
import { workOperation } from './actions';
const href = '#/p/p_1/task/t_1';
beforeEach(() => { vi.resetAllMocks(); });
describe('explicit Overview actions use current controller evidence', () => {
  it('retries only the same failed latest run, never a newer run', async () => {
    fake.listTaskRuns.mockResolvedValue([{ id: 'run_1', state: 'failed' }]);
    await workOperation({ operation: 'retry', href, runId: 'run_1' });
    expect(fake.startRun).toHaveBeenCalledWith('p_1', 't_1');
    fake.startRun.mockClear();
    fake.listTaskRuns.mockResolvedValue([{ id: 'run_1', state: 'failed' }, { id: 'run_new', state: 'running' }]);
    await expect(workOperation({ operation: 'retry', href, runId: 'run_1' })).rejects.toThrow('newer run');
    expect(fake.startRun).not.toHaveBeenCalled();
  });
  it('sends only a still-offered blocker choice', async () => {
    fake.listTaskRuns.mockResolvedValue([{ id: 'run_1', state: 'blocked', blocker: { options: ['Continue'] } }]);
    await workOperation({ operation: 'reply', href, runId: 'run_1', answer: 'Continue' });
    expect(fake.sendInput).toHaveBeenCalledWith('p_1', 'run_1', 'Continue');
    fake.sendInput.mockClear();
    await expect(workOperation({ operation: 'reply', href, runId: 'run_1', answer: 'Execute arbitrary text' })).rejects.toThrow('blocker changed');
    expect(fake.sendInput).not.toHaveBeenCalled();
  });
  it('re-reads the incoming item instead of trusting a projected repository address', async () => {
    fake.waiting.mockResolvedValue({ items: [{ sourceRef: 'source', repository: 'https://github.com/acme/app', github: 'acme/app' }] });
    await workOperation({ operation: 'link', sourceRef: 'source', path: '/clone' });
    expect(fake.linkWaiting).toHaveBeenCalledWith('/clone', 'https://github.com/acme/app');
    fake.waiting.mockResolvedValue({ items: [] });
    await expect(workOperation({ operation: 'clone', sourceRef: 'source' })).rejects.toThrow('no longer waiting');
    expect(fake.githubAddRepo).not.toHaveBeenCalled();
  });
  it('rejects external and malformed task/run references before reads', async () => {
    await expect(workOperation({ operation: 'retry', href: 'https://example.test', runId: 'run_1' })).rejects.toThrow('invalid');
    await expect(workOperation({ operation: 'retry', href, runId: '../escape' })).rejects.toThrow('invalid');
    expect(fake.listTaskRuns).not.toHaveBeenCalled();
  });
});
