create policy operator_email_deny_direct on licensing_private.operator_emails
for all to authenticated using (false) with check (false);

create function licensing_private.cancel(p_id uuid,p_reason text) returns jsonb
language plpgsql security definer set search_path='' as $$
declare actor_id uuid:=licensing_private.operator_id(); op public.licensing_operations;
begin
  if length(btrim(coalesce(p_reason,''))) not between 3 and 1000 then raise exception 'Record why this request is cancelled'; end if;
  select * into op from public.licensing_operations where id=p_id for update;
  if not found or op.state='completed' then raise exception 'A completed issue cannot be cancelled; close its entitlement instead'; end if;
  if op.state='cancelled' then return to_jsonb(op); end if;
  perform 1 from public.licensing_entitlements where id=op.entitlement_id for update;
  update public.licensing_operations set state='cancelled',completed_at=now() where id=op.id returning * into op;
  if op.kind='issue' then
    update public.licensing_entitlements set status='closed',closed_at=now(),close_reason=p_reason where id=op.entitlement_id;
  end if;
  insert into public.licensing_events(entitlement_id,operation_id,actor,action,details)
    values(op.entitlement_id,op.id,actor_id,'cancelled-before-issuance',jsonb_build_object('reason',p_reason));
  return to_jsonb(op);
end $$;
create function public.licensing_cancel(p_id uuid,p_reason text) returns jsonb
language sql security invoker set search_path='' as $$ select licensing_private.cancel(p_id,p_reason) $$;
revoke all on function licensing_private.cancel(uuid,text),public.licensing_cancel(uuid,text) from public,anon,authenticated;
grant execute on function licensing_private.cancel(uuid,text),public.licensing_cancel(uuid,text) to authenticated;

create function licensing_private.restore_register(p_actor uuid,p_session uuid,p_snapshot jsonb,p_source_sha256 text)
returns jsonb language plpgsql security definer set search_path='' as $$
declare row_count bigint; op public.licensing_operations; ent public.licensing_entitlements;
begin
  perform licensing_private.operator_id(p_actor,p_session);
  -- The deployment/recovery operator must provision an empty destination. This
  -- path never merges, truncates or overwrites an existing register.
  lock table public.licensing_entitlements,public.licensing_operations,public.licensing_artifacts,public.licensing_events in access exclusive mode;
  if exists(select 1 from public.licensing_entitlements) or exists(select 1 from public.licensing_operations)
    or exists(select 1 from public.licensing_artifacts) or exists(select 1 from public.licensing_events) then
    raise exception 'Restore requires an empty register; existing history is never overwritten'; end if;
  if p_snapshot->>'product'<>'werkbord-licensing-register' or p_snapshot->>'schema'<>'1'
    or p_source_sha256 !~ '^[0-9a-f]{64}$'
    or p_snapshot->'issuers' is distinct from (select jsonb_agg(to_jsonb(i) order by fingerprint) from public.licensing_issuers i) then
    raise exception 'Backup schema or pinned issuer registry differs from this deployment'; end if;
  insert into public.licensing_entitlements select * from jsonb_populate_recordset(null::public.licensing_entitlements,p_snapshot->'entitlements');
  insert into public.licensing_operations select * from jsonb_populate_recordset(null::public.licensing_operations,p_snapshot->'operations');
  insert into public.licensing_artifacts select * from jsonb_populate_recordset(null::public.licensing_artifacts,p_snapshot->'artifacts');
  insert into public.licensing_events overriding system value select * from jsonb_populate_recordset(null::public.licensing_events,p_snapshot->'events');
  perform setval(pg_get_serial_sequence('public.licensing_events','id'),coalesce((select max(id) from public.licensing_events),1),exists(select 1 from public.licensing_events));
  select count(*) into row_count from public.licensing_artifacts;
  -- Receipt links to an existing entitlement; an empty snapshot needs no event.
  select * into ent from public.licensing_entitlements order by created_at limit 1;
  if found then
    insert into public.licensing_operations(request_reference,kind,entitlement_id,revision,parameters,state,reason,actor,completed_at)
      values('restore:'||p_source_sha256,'import',ent.id,greatest(ent.current_revision,1),jsonb_build_object('sourceSHA256',p_source_sha256),
        'completed','Register restored from a verified independent snapshot',p_actor,now()) returning * into op;
    insert into public.licensing_events(entitlement_id,operation_id,actor,action,details)
      values(ent.id,op.id,p_actor,'register-restored',jsonb_build_object('sourceSHA256',p_source_sha256,'artifacts',row_count));
  end if;
  return jsonb_build_object('restoredArtifacts',row_count,'sourceSHA256',p_source_sha256);
end $$;
create function public.licensing_restore(p_actor uuid,p_session uuid,p_snapshot jsonb,p_source_sha256 text)
returns jsonb language sql security invoker set search_path='' as $$
  select licensing_private.restore_register(p_actor,p_session,p_snapshot,p_source_sha256)
$$;
revoke all on function licensing_private.restore_register(uuid,uuid,jsonb,text),public.licensing_restore(uuid,uuid,jsonb,text) from public,anon,authenticated;
grant execute on function licensing_private.restore_register(uuid,uuid,jsonb,text),public.licensing_restore(uuid,uuid,jsonb,text) to service_role;
