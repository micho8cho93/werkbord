import {describe,it,expect} from 'vitest';
import {workGroup,blockingTasks} from './work-queue';
import type {Task,Run} from './types';
const task=(o:Partial<Task>={}):Task=>({id:'one',projectId:'p',title:'Design',description:'',state:'backlog',position:1,execution:{},version:1,createdAt:'',updatedAt:'',...o});
const now=Date.parse('2026-10-30T12:00:00Z');
describe('Work queue derives categories from existing facts',()=>{
 it('groups active work and attention before future planning',()=>{
 expect(workGroup(task({state:'doing',plan:{start:'2027-01-01'}}),[],undefined,undefined,now)).toBe('now');
 expect(workGroup(task({state:'review'}),[],undefined,undefined,now)).toBe('now');
 expect(workGroup(task(),[],{state:'blocked'} as Run,undefined,now)).toBe('now');
 expect(workGroup(task(),[],undefined,'high',now)).toBe('now');
 });
 it('puts ready tasks in Next and future or lower priority work in Later',()=>{
 expect(workGroup(task(),[],undefined,undefined,now)).toBe('next');
 expect(workGroup(task({plan:{start:'2026-11-02'}}),[],undefined,undefined,now)).toBe('later');
 expect(workGroup(task(),[],undefined,'low',now)).toBe('later');
 expect(workGroup(task({state:'done'}),[],undefined,'high',now)).toBe('done');
 });
 it('names unfinished and unavailable dependencies, including work in review',()=>{
 const dependent=task({id:'two',orchestration:{enabled:false,dependencies:['one','missing'],expectedPaths:[]}});
 const first=task({state:'review'});
 expect(blockingTasks(dependent,[first,dependent])).toEqual([{id:'one',title:'Design'},{id:'missing',title:'Unavailable task'}]);
 expect(workGroup(dependent,[first,dependent],undefined,undefined,now)).toBe('later');
 expect(workGroup(task({orchestration:{enabled:false,dependencies:['one'],expectedPaths:[]}}),[task({state:'done'})],undefined,undefined,now)).toBe('next');
 });
});

describe('dependency execution facts agree with Calendar and the scheduler', () => {
 it('accepts a successful run before review is marked Done, but waits for a newer retry', () => {
  const first = task({state:'review'});
  const dependent = task({id:'two',orchestration:{enabled:true,dependencies:['one'],expectedPaths:[]}});
  const latest = {one:{state:'completed'} as Run};
  expect(blockingTasks(dependent,[first,dependent],latest)).toEqual([]);
  expect(workGroup(dependent,[first,dependent],undefined,undefined,now,latest)).toBe('next');
  for (const state of ['starting','running','waiting_for_user','blocked','failed','stopped'] as const) {
   expect(blockingTasks(dependent,[first,dependent],{one:{state} as Run})).toEqual([{id:'one',title:'Design'}]);
   expect(blockingTasks(dependent,[{...first,state:'done'},dependent],{one:{state} as Run})).toHaveLength(1);
  }
 });
});
