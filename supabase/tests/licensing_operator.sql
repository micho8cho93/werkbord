-- Run against a development destination (or transactionally against the new
-- empty register). All fixtures and changes roll back, including operator ACLs.
begin;
insert into auth.users(id,email,email_confirmed_at) values
 ('00000000-0000-4000-8000-000000000101','licensing-fixture@example.invalid',now()),
 ('00000000-0000-4000-8000-000000000102','other-fixture@example.invalid',now());
insert into auth.sessions(id,user_id,aal) values
 ('00000000-0000-4000-8000-000000000201','00000000-0000-4000-8000-000000000101','aal2'),
 ('00000000-0000-4000-8000-000000000202','00000000-0000-4000-8000-000000000102','aal2');
insert into licensing_private.operator_emails(email) values('licensing-fixture@example.invalid');
set local role authenticated;
select set_config('request.jwt.claims','{"sub":"00000000-0000-4000-8000-000000000101","aal":"aal2","session_id":"00000000-0000-4000-8000-000000000201"}',true);
do $$ declare op jsonb; again jsonb; begin
 op:=public.licensing_prepare('fixture-issue','issue',null,0,'Fixture customer',3,null,null,'Fixture approved manually',null);
 again:=public.licensing_prepare('fixture-issue','issue',null,0,'Fixture customer',3,null,null,'Fixture approved manually',null);
 if op->>'id'<>again->>'id' then raise exception 'Idempotency failed'; end if;
 begin
   perform public.licensing_prepare('fixture-issue','issue',null,0,'Fixture customer',10,null,null,'Fixture approved manually',null);
   raise exception 'Conflicting reference was accepted';
 exception when raise_exception then if sqlerrm='Conflicting reference was accepted' then raise; end if; end;
 begin
   update public.licensing_operations set state='completed';
   raise exception 'Direct history update was accepted';
 exception when insufficient_privilege then null; end;
 begin
   perform public.licensing_complete((op->>'id')::uuid,'00000000-0000-4000-8000-000000000101','00000000-0000-4000-8000-000000000201','{}','bad');
   raise exception 'Client bypassed signature verifier';
 exception when insufficient_privilege then null; end;
end $$;
reset role;
-- The service-only completion assumes its Edge verifier has checked the actual
-- Ed25519 signature. Go and TypeScript tests exercise that separate boundary.
set local role service_role;
do $$ declare op public.licensing_operations; doc text; result jsonb; begin
 select * into op from public.licensing_operations where request_reference='fixture-issue';
 doc:=jsonb_build_object('claims',op.claims,'signature','fixture-only-not-a-real-license')::text;
 result:=public.licensing_complete(op.id,'00000000-0000-4000-8000-000000000101','00000000-0000-4000-8000-000000000201',doc,'b66484e825f6148e826f46e48f7134d7d00bb2ea139a700182dca1ef5ddd502b');
 if result->>'license_id'<>op.claims->>'id' then raise exception 'Artifact identity changed'; end if;
 if public.licensing_complete(op.id,'00000000-0000-4000-8000-000000000101','00000000-0000-4000-8000-000000000201',doc,'b66484e825f6148e826f46e48f7134d7d00bb2ea139a700182dca1ef5ddd502b')->>'sha256'<>result->>'sha256' then raise exception 'Completion retry changed bytes'; end if;
end $$;
reset role;
set local role authenticated;
do $$ declare ent public.licensing_entitlements; op jsonb; begin
 select * into ent from public.licensing_entitlements where customer='Fixture customer';
 op:=public.licensing_prepare('fixture-expand','expand',ent.id,1,null,10,null,null,'Customer requested ten seats',null);
 if op->'claims'->>'seats'<>'10' or op->'claims'->>'customer'<>'Fixture customer' then raise exception 'Expansion lost the customer or seat allowance'; end if;
 begin
  perform public.licensing_record_action('fixture-close-pending','close',ent.id,1,'Customer is leaving',null);
  raise exception 'Closure ignored pending issuance';
 exception when raise_exception then if sqlerrm='Closure ignored pending issuance' then raise; end if; end;
 perform public.licensing_cancel((op->>'id')::uuid,'Customer delayed the expansion');
 perform public.licensing_record_action('fixture-close','close',ent.id,1,'Customer requested shutdown',null);
 begin
  perform public.licensing_prepare('fixture-after-close','expand',ent.id,1,null,10,null,null,'Attempt after closure',null);
  raise exception 'Closed license was expanded';
 exception when raise_exception then if sqlerrm='Closed license was expanded' then raise; end if; end;
 if (select count(*) from public.licensing_artifacts where entitlement_id=ent.id)<>1 then raise exception 'Original file was deleted'; end if;
end $$;
-- Round-trip a consistent register into an empty destination and verify that
-- original artifacts, identities and operation retries survive. Only fixture
-- rows are removed inside this rolled-back transaction.
select set_config('licensing.test_snapshot',public.licensing_export()::text,true);
reset role;
delete from public.licensing_events where entitlement_id in(select entitlement_id from public.licensing_operations where request_reference='fixture-issue');
delete from public.licensing_artifacts where entitlement_id in(select entitlement_id from public.licensing_operations where request_reference='fixture-issue');
delete from public.licensing_operations where entitlement_id in(select id from public.licensing_entitlements where customer='Fixture customer');
delete from public.licensing_entitlements where customer='Fixture customer';
set local role service_role;
do $$ declare result jsonb; begin
 result:=public.licensing_restore('00000000-0000-4000-8000-000000000101','00000000-0000-4000-8000-000000000201',current_setting('licensing.test_snapshot')::jsonb,repeat('a',64));
 if result->>'restoredArtifacts'<>'1' then raise exception 'Restore lost the original artifact'; end if;
 begin
  perform public.licensing_restore('00000000-0000-4000-8000-000000000101','00000000-0000-4000-8000-000000000201',current_setting('licensing.test_snapshot')::jsonb,repeat('a',64));
  raise exception 'Restore overwrote nonempty history';
 exception when raise_exception then if sqlerrm='Restore overwrote nonempty history' then raise; end if; end;
end $$;
reset role;
set local role authenticated;
do $$ declare restored jsonb; original jsonb:=current_setting('licensing.test_snapshot')::jsonb; begin
 restored:=public.licensing_export();
 if restored->'artifacts'<>original->'artifacts' then raise exception 'Restore changed original signed bytes or identity'; end if;
 if (select count(*) from public.licensing_operations where request_reference='fixture-issue')<>1 then raise exception 'Restore lost the idempotency reference'; end if;
end $$;
select set_config('request.jwt.claims','{"sub":"00000000-0000-4000-8000-000000000101","aal":"aal1","session_id":"00000000-0000-4000-8000-000000000201"}',true);
do $$ begin begin perform public.licensing_export();raise exception 'MFA was not enforced';exception when insufficient_privilege then null;end;end $$;
select set_config('request.jwt.claims','{"sub":"00000000-0000-4000-8000-000000000102","aal":"aal2","session_id":"00000000-0000-4000-8000-000000000202"}',true);
do $$ begin begin perform public.licensing_export();raise exception 'Another user accessed the register';exception when insufficient_privilege then null;end;end $$;
reset role;
delete from auth.sessions where id='00000000-0000-4000-8000-000000000201';
set local role authenticated;
select set_config('request.jwt.claims','{"sub":"00000000-0000-4000-8000-000000000101","aal":"aal2","session_id":"00000000-0000-4000-8000-000000000201"}',true);
do $$ begin begin perform public.licensing_export();raise exception 'Deleted session retained access';exception when insufficient_privilege then null;end;end $$;
reset role;
select 'licensing operator transaction, idempotency, lifecycle and authorisation checks passed' as result;
rollback;
