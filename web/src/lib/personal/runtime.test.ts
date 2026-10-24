import { describe, expect, it, vi } from 'vitest';
import fixtures from '../../../../internal/assistant/testdata/personal_apps.json';
import { collectionSchema, validateWorkspace, type PersonalRecord } from './schema';
import { calculatedValues, countdown, hierarchyRows, metricValue } from './runtime';
import { workspaceSource } from './library';
import { WorkspaceSession } from './session.svelte';
import { personalAPI } from './api';
const cid = 'pcl_abcdefghijklmnop', wid = 'pws_abcdefghijklmnop';
const meta = {revision:1,createdAt:'2026-10-24T12:00:00Z',updatedAt:'2026-10-24T12:00:00Z'};
function app(index: number) {
  const source = structuredClone(fixtures[index]);
  const collection = collectionSchema.parse({...source.change.collections[0].spec,...meta,id:cid});
  const workspace = validateWorkspace({...source.change.workspace,...meta,id:wid,collectionIds:[cid],definition:{...source.change.workspace.definition,views:source.change.workspace.definition.views.map(v => ({...v,collectionId:cid}))}},[collection]);
  const record: PersonalRecord = {...meta,id:'prec_abcdefghijklmnop',collectionId:cid,values:source.values as unknown as PersonalRecord['values']};
  return {workspace,collection,record};
}
describe('composable workspace engine', () => {
  it.each(fixtures.map((f,i) => [f.name,i] as const))('renders the validated %s configuration with the installed registry', (_,index) => {
    const {workspace} = app(index);
    expect(workspaceSource(workspace)).toContain('Collection(');
  });
  it('calculates from shared records, rejects cycles and treats zero division as unavailable', () => {
    const {workspace,collection,record} = app(0);
    expect(calculatedValues(collection, record.values).weighted).toBe(600);
    expect(record.values.weighted).toBeUndefined();
    const metrics = workspace.definition.views.find(v => v.kind === 'metric')!;
    expect(metricValue(metrics,[record])).toBe(1200);
    collection.fields.find(f => f.key === 'weighted')!.formula!.args![0].field = 'weighted';
    expect(() => validateWorkspace(workspace,[collection])).toThrow();
    const marketing = app(1);
    expect(calculatedValues(marketing.collection,{...marketing.record.values,impressions:0}).ctr).toBeUndefined();
  });
  it('rejects executable actions and fields outside the view', () => {
    const {workspace,collection} = app(0);
    workspace.definition.views[0].actions![0].steps[0].values = {unknown:'secret'};
    expect(() => validateWorkspace(workspace,[collection])).toThrow();
  });
  it('keeps cycles and missing parents visible exactly once and handles deep hierarchies', () => {
    const {record} = app(3);
    const records = Array.from({length:10000},(_,i) => ({...record,id:`r${i}`,values:{title:`Choice ${i}`,parent:`r${i-1}`}}));
    records[0].values.parent = 'r9999';
    expect(hierarchyRows(records,'parent')).toHaveLength(10000);
    expect(countdown('2026-10-25',Date.parse('2026-10-24T12:00:00Z'))).toBe('1 day remaining');
  });
  it('dispatches only saved action IDs with exact revisions and updates every view', async () => {
    const {workspace,collection,record} = app(0);
    const api = {...personalAPI,executeAction:vi.fn().mockResolvedValue({saved:[{...record,revision:2,values:{...record.values,stage:'Contacted'}}],deleted:[]})};
    const session = new WorkspaceSession(api); session.workspace=workspace;session.collections=[collection];session.records={[cid]:[record]};
    expect(await session.action('leads','contact',[record.id],false)).toBe(false);
    expect(api.executeAction).not.toHaveBeenCalled();
    expect(await session.action('leads','contact',[record.id],true)).toBe(true);
    expect(api.executeAction).toHaveBeenCalledWith(wid,'leads','contact',{workspaceRevision:1,collectionRevision:1,confirmed:true,records:[{id:record.id,revision:1}]});
    expect(session.records[cid][0].values.stage).toBe('Contacted');
    session.readOnly=true;
    expect(await session.action('leads','contact',[record.id],true)).toBe(false);
    expect(api.executeAction).toHaveBeenCalledTimes(1);
  });
});
