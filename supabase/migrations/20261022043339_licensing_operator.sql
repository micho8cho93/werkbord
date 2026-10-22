-- Vendor register only. No customer installation contacts this database.
create schema if not exists licensing_private;
revoke all on schema licensing_private from public;
grant usage on schema licensing_private to authenticated, service_role;
alter default privileges in schema licensing_private revoke execute on functions from public;

create table licensing_private.operator_emails (
  email text primary key check (email = lower(email)),
  enabled boolean not null default true
);
alter table licensing_private.operator_emails enable row level security;

create function licensing_private.operator_id(p_user uuid default auth.uid(), p_session uuid default null)
returns uuid language plpgsql stable security definer set search_path = '' as $$
declare session_uuid uuid;
begin
  if p_user is null then raise exception 'Operator sign-in is required' using errcode='42501'; end if;
  if p_session is null then
    if coalesce(auth.jwt()->>'aal','') <> 'aal2' then
      raise exception 'Verify your authenticator code first' using errcode='42501';
    end if;
    session_uuid := (auth.jwt()->>'session_id')::uuid;
  else
    -- Explicit actor/session is reserved for the verified completion service.
    if current_setting('role',true) <> 'service_role' then
      raise exception 'Service authorisation required' using errcode='42501';
    end if;
    session_uuid := p_session;
  end if;
  if not exists (
    select 1 from auth.users u join licensing_private.operator_emails o on o.email=lower(u.email)
    join auth.sessions s on s.user_id=u.id
    where u.id=p_user and o.enabled and u.email_confirmed_at is not null
      and not u.is_anonymous and s.id=session_uuid and s.aal='aal2'
      and (s.not_after is null or s.not_after>now())
  ) then raise exception 'This session is not an authorised operator' using errcode='42501'; end if;
  return p_user;
end $$;
revoke all on function licensing_private.operator_id(uuid,uuid) from public;
grant execute on function licensing_private.operator_id(uuid,uuid) to authenticated,service_role;

create table public.licensing_issuers (
  fingerprint text primary key check (fingerprint ~ '^[0-9a-f]{64}$'),
  public_key text not null unique,
  can_issue boolean not null default false
);
insert into public.licensing_issuers(fingerprint,public_key,can_issue)
values ('b66484e825f6148e826f46e48f7134d7d00bb2ea139a700182dca1ef5ddd502b',
  'ChctbT2EmgTYU37lb0e1_KkzSO9GR32pkRdcTR-UoYU',true);

create table public.licensing_entitlements (
  id uuid primary key default gen_random_uuid(),
  customer text not null check (length(btrim(customer)) between 1 and 256),
  status text not null check (status in ('pending','active','closed')),
  current_revision integer not null default 0 check (current_revision>=0),
  created_at timestamptz not null default now(),
  created_by uuid not null,
  closed_at timestamptz,
  close_reason text
);
create table public.licensing_operations (
  id uuid primary key default gen_random_uuid(),
  request_reference text not null unique check (length(btrim(request_reference)) between 1 and 128),
  kind text not null check (kind in ('issue','expand','close','delivery','import')),
  entitlement_id uuid not null references public.licensing_entitlements(id),
  revision integer not null check (revision>0),
  parameters jsonb not null,
  claims jsonb,
  state text not null check (state in ('pending','completed','cancelled')),
  reason text not null check (length(btrim(reason)) between 3 and 1000),
  actor uuid not null,
  created_at timestamptz not null default now(),
  completed_at timestamptz
);
create unique index licensing_one_pending_revision on public.licensing_operations(entitlement_id)
where state='pending';
create index licensing_operations_entitlement on public.licensing_operations(entitlement_id,created_at);
create table public.licensing_artifacts (
  license_id text primary key,
  entitlement_id uuid not null references public.licensing_entitlements(id),
  revision integer not null check (revision>0),
  document text not null check (octet_length(document)<=16384),
  sha256 text not null check (sha256 ~ '^[0-9a-f]{64}$'),
  issuer_fingerprint text not null references public.licensing_issuers(fingerprint),
  claims jsonb not null,
  operation_id uuid not null unique references public.licensing_operations(id),
  recorded_at timestamptz not null default now(),
  unique(entitlement_id,revision),
  check (sha256=encode(extensions.digest(convert_to(document,'UTF8'),'sha256'),'hex')),
  check ((document::jsonb)->'claims'=claims)
);
create table public.licensing_events (
  id bigint generated always as identity primary key,
  entitlement_id uuid not null references public.licensing_entitlements(id),
  operation_id uuid not null references public.licensing_operations(id),
  actor uuid not null,
  action text not null,
  details jsonb not null default '{}'::jsonb,
  created_at timestamptz not null default now()
);
create index licensing_events_entitlement on public.licensing_events(entitlement_id,id);
create index licensing_events_operation on public.licensing_events(operation_id);

do $$ declare t text; begin
  foreach t in array array['licensing_issuers','licensing_entitlements','licensing_operations','licensing_artifacts','licensing_events'] loop
    execute format('alter table public.%I enable row level security',t);
    execute format('revoke all on public.%I from anon,authenticated',t);
    execute format('grant select on public.%I to authenticated',t);
    execute format('create policy operator_read on public.%I for select to authenticated using ((select licensing_private.operator_id()) is not null)',t);
  end loop;
end $$;

create function licensing_private.prepare(p_reference text,p_kind text,p_entitlement uuid,p_expected_revision integer,
  p_customer text,p_seats integer,p_expires_at timestamptz,p_support_ends_at timestamptz,p_reason text,p_order_reference text)
returns jsonb language plpgsql security definer set search_path='' as $$
declare actor_id uuid := licensing_private.operator_id(); existing public.licensing_operations;
  entitlement public.licensing_entitlements; op public.licensing_operations; previous public.licensing_artifacts;
  params jsonb; payload jsonb; issued timestamptz := date_trunc('second',now()); next_revision integer;
begin
  if p_kind not in ('issue','expand') or p_reference is null or length(btrim(p_reference)) not between 1 and 128
    or p_reason is null or length(btrim(p_reason)) not between 3 and 1000
    or p_seats is null or p_seats not between 1 and 1000000
    or length(coalesce(p_order_reference,''))>256 then raise exception 'Check the request reference, seats and reason'; end if;
  params := jsonb_build_object('kind',p_kind,'entitlement',p_entitlement,'expectedRevision',p_expected_revision,
    'customer',p_customer,'seats',p_seats,'expiresAt',p_expires_at,'supportEndsAt',p_support_ends_at,'reason',p_reason,'orderReference',p_order_reference);
  perform pg_advisory_xact_lock(hashtextextended(p_reference,0));
  select * into existing from public.licensing_operations where request_reference=p_reference;
  if found then
    if existing.parameters<>params then raise exception 'This reference already has different terms'; end if;
    return to_jsonb(existing);
  end if;
  if p_kind='issue' then
    if p_entitlement is not null or p_expected_revision<>0 or p_expected_revision is null or p_customer is null
      or length(btrim(p_customer)) not between 1 and 256 then raise exception 'A new issue needs a customer and revision zero'; end if;
    insert into public.licensing_entitlements(customer,status,created_by) values(p_customer,'pending',actor_id) returning * into entitlement;
    next_revision:=1;
  else
    select * into entitlement from public.licensing_entitlements where id=p_entitlement for update;
    if not found or entitlement.status<>'active' or entitlement.current_revision is distinct from p_expected_revision then
      raise exception 'The entitlement changed or is closed; refresh before retrying'; end if;
    select * into previous from public.licensing_artifacts where entitlement_id=entitlement.id and revision=entitlement.current_revision;
    if p_seats<=(previous.claims->>'seats')::integer then raise exception 'An expansion must increase the seat allowance'; end if;
    if p_customer is not null or p_expires_at is not null or p_support_ends_at is not null then raise exception 'Expansion preserves the existing customer and dates'; end if;
    p_customer:=entitlement.customer;
    p_expires_at:=(previous.claims->>'expiresAt')::timestamptz;
    p_support_ends_at:=(previous.claims->>'supportEndsAt')::timestamptz;
    next_revision:=entitlement.current_revision+1;
  end if;
  if p_expires_at<=issued or p_support_ends_at<issued then raise exception 'The selected dates are already in the past; issue a renewal separately'; end if;
  payload:=jsonb_build_object('schema',2,'product','werkbord-team','id','lic_'||replace(gen_random_uuid()::text,'-',''),
    'customer',p_customer,'edition','team','seats',p_seats,'issuedAt',to_char(issued at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS"Z"'));
  if p_expires_at is not null then payload:=payload||jsonb_build_object('expiresAt',to_char(p_expires_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS"Z"')); end if;
  if p_support_ends_at is not null then payload:=payload||jsonb_build_object('supportEndsAt',to_char(p_support_ends_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS"Z"')); end if;
  insert into public.licensing_operations(request_reference,kind,entitlement_id,revision,parameters,claims,state,reason,actor)
    values(p_reference,p_kind,entitlement.id,next_revision,params,payload,'pending',p_reason,actor_id) returning * into op;
  insert into public.licensing_events(entitlement_id,operation_id,actor,action,details)
    values(entitlement.id,op.id,actor_id,'prepared',jsonb_build_object('revision',next_revision));
  return to_jsonb(op);
end $$;

create function public.licensing_prepare(p_reference text,p_kind text,p_entitlement uuid,p_expected_revision integer,
  p_customer text,p_seats integer,p_expires_at timestamptz,p_support_ends_at timestamptz,p_reason text,p_order_reference text)
returns jsonb language sql security invoker set search_path='' as $$
  select licensing_private.prepare(p_reference,p_kind,p_entitlement,p_expected_revision,p_customer,p_seats,p_expires_at,p_support_ends_at,p_reason,p_order_reference)
$$;

create function public.licensing_operation(p_id uuid) returns jsonb language sql security invoker set search_path='' as $$
  select to_jsonb(o) from public.licensing_operations o where o.id=p_id
$$;

create function licensing_private.complete(p_id uuid,p_actor uuid,p_session uuid,p_document text,p_fingerprint text)
returns jsonb language plpgsql security definer set search_path='' as $$
declare op public.licensing_operations; ent public.licensing_entitlements; artifact public.licensing_artifacts;
begin
  perform licensing_private.operator_id(p_actor,p_session);
  select * into op from public.licensing_operations where id=p_id for update;
  if not found then raise exception 'Operation not found'; end if;
  if op.state='completed' then
    select * into artifact from public.licensing_artifacts where operation_id=op.id;
    if artifact.document is distinct from p_document then raise exception 'Completed operation has a different artifact'; end if;
    return to_jsonb(artifact);
  end if;
  if op.state<>'pending' or op.kind not in ('issue','expand') or op.claims is distinct from (p_document::jsonb)->'claims'
    or not exists(select 1 from public.licensing_issuers where fingerprint=p_fingerprint and can_issue) then
    raise exception 'The signature does not match a pending authorised issue'; end if;
  select * into ent from public.licensing_entitlements where id=op.entitlement_id for update;
  if ent.status='closed' or ent.current_revision<>op.revision-1 then raise exception 'The entitlement changed before signing completed'; end if;
  insert into public.licensing_artifacts(license_id,entitlement_id,revision,document,sha256,issuer_fingerprint,claims,operation_id)
    values(op.claims->>'id',ent.id,op.revision,p_document,encode(extensions.digest(convert_to(p_document,'UTF8'),'sha256'),'hex'),p_fingerprint,op.claims,op.id)
    returning * into artifact;
  update public.licensing_entitlements set status='active',current_revision=op.revision where id=ent.id;
  update public.licensing_operations set state='completed',completed_at=now() where id=op.id;
  insert into public.licensing_events(entitlement_id,operation_id,actor,action,details)
    values(ent.id,op.id,p_actor,case when op.kind='expand' then 'expanded' else 'issued' end,
      jsonb_build_object('revision',op.revision,'licenseId',artifact.license_id,'sha256',artifact.sha256));
  return to_jsonb(artifact);
end $$;
-- Signature verification is performed by the Edge Function before this service-only call.
create function public.licensing_complete(p_id uuid,p_actor uuid,p_session uuid,p_document text,p_fingerprint text)
returns jsonb language sql security invoker set search_path='' as $$
  select licensing_private.complete(p_id,p_actor,p_session,p_document,p_fingerprint)
$$;

create function licensing_private.record_action(p_reference text,p_kind text,p_entitlement uuid,p_expected_revision integer,
  p_reason text,p_channel text) returns jsonb language plpgsql security definer set search_path='' as $$
declare actor_id uuid:=licensing_private.operator_id(); ent public.licensing_entitlements;
  existing public.licensing_operations; op public.licensing_operations; params jsonb;
begin
  if p_kind not in ('close','delivery') or length(btrim(coalesce(p_reference,''))) not between 1 and 128
    or length(btrim(coalesce(p_reason,''))) not between 3 and 1000
    or (p_kind='delivery' and p_channel not in ('email','manual','customer-download'))
    or (p_kind='delivery' and p_channel is null) then raise exception 'Provide a reference, reason and supported action'; end if;
  params:=jsonb_build_object('kind',p_kind,'entitlement',p_entitlement,'expectedRevision',p_expected_revision,'reason',p_reason,'channel',p_channel);
  perform pg_advisory_xact_lock(hashtextextended(p_reference,0));
  select * into existing from public.licensing_operations where request_reference=p_reference;
  if found then
    if existing.parameters<>params then raise exception 'This reference already has different parameters'; end if;
    return to_jsonb(existing);
  end if;
  select * into ent from public.licensing_entitlements where id=p_entitlement for update;
  if not found or ent.current_revision is distinct from p_expected_revision or ent.status<>'active' then
    raise exception 'The entitlement changed or is closed; refresh before retrying'; end if;
  if exists(select 1 from public.licensing_operations where entitlement_id=ent.id and state='pending') then
    raise exception 'Finish or cancel the pending issue first'; end if;
  insert into public.licensing_operations(request_reference,kind,entitlement_id,revision,parameters,state,reason,actor,completed_at)
    values(p_reference,p_kind,ent.id,ent.current_revision,params,'completed',p_reason,actor_id,now()) returning * into op;
  if p_kind='close' then update public.licensing_entitlements set status='closed',closed_at=now(),close_reason=p_reason where id=ent.id; end if;
  insert into public.licensing_events(entitlement_id,operation_id,actor,action,details)
    values(ent.id,op.id,actor_id,case when p_kind='close' then 'closed-in-register' else 'delivery-recorded' end,
      jsonb_build_object('revision',ent.current_revision,'reason',p_reason,'channel',p_channel,'offlineEnforcement',false));
  return to_jsonb(op);
end $$;
create function public.licensing_record_action(p_reference text,p_kind text,p_entitlement uuid,p_expected_revision integer,p_reason text,p_channel text)
returns jsonb language sql security invoker set search_path='' as $$
  select licensing_private.record_action(p_reference,p_kind,p_entitlement,p_expected_revision,p_reason,p_channel)
$$;

create function licensing_private.import_artifact(p_actor uuid,p_session uuid,p_document text,p_fingerprint text,p_source text)
returns jsonb language plpgsql security definer set search_path='' as $$
declare c jsonb:=(p_document::jsonb)->'claims'; artifact public.licensing_artifacts; ent public.licensing_entitlements;
  op public.licensing_operations; hash text:=encode(extensions.digest(convert_to(p_document,'UTF8'),'sha256'),'hex');
begin
  perform licensing_private.operator_id(p_actor,p_session);
  if length(btrim(coalesce(p_source,''))) not between 3 and 1000 then raise exception 'Provide the import provenance'; end if;
  perform pg_advisory_xact_lock(hashtextextended('import:'||(c->>'id'),0));
  select * into artifact from public.licensing_artifacts where license_id=c->>'id';
  if found then
    if artifact.sha256<>hash then raise exception 'This license ID already has different bytes'; end if;
    return to_jsonb(artifact);
  end if;
  insert into public.licensing_entitlements(customer,status,current_revision,created_by)
    values(c->>'customer','active',1,p_actor) returning * into ent;
  insert into public.licensing_operations(request_reference,kind,entitlement_id,revision,parameters,claims,state,reason,actor,completed_at)
    values('import:'||hash,'import',ent.id,1,jsonb_build_object('sha256',hash,'source',p_source),c,'completed',p_source,p_actor,now()) returning * into op;
  insert into public.licensing_artifacts(license_id,entitlement_id,revision,document,sha256,issuer_fingerprint,claims,operation_id)
    values(c->>'id',ent.id,1,p_document,hash,p_fingerprint,c,op.id) returning * into artifact;
  insert into public.licensing_events(entitlement_id,operation_id,actor,action,details)
    values(ent.id,op.id,p_actor,'imported',jsonb_build_object('source',p_source,'sha256',hash,'deliveryAssumed',false,'paymentAssumed',false));
  return to_jsonb(artifact);
end $$;
create function public.licensing_import(p_actor uuid,p_session uuid,p_document text,p_fingerprint text,p_source text)
returns jsonb language sql security invoker set search_path='' as $$
  select licensing_private.import_artifact(p_actor,p_session,p_document,p_fingerprint,p_source)
$$;

create function licensing_private.export_register() returns jsonb language plpgsql security definer set search_path='' as $$
begin
  perform licensing_private.operator_id();
  return jsonb_build_object('schema',1,'product','werkbord-licensing-register','exportedAt',now(),
    'issuers',coalesce((select jsonb_agg(to_jsonb(t) order by fingerprint) from public.licensing_issuers t),'[]'::jsonb),
    'entitlements',coalesce((select jsonb_agg(to_jsonb(t) order by id) from public.licensing_entitlements t),'[]'::jsonb),
    'operations',coalesce((select jsonb_agg(to_jsonb(t) order by id) from public.licensing_operations t),'[]'::jsonb),
    'artifacts',coalesce((select jsonb_agg(to_jsonb(t) order by license_id) from public.licensing_artifacts t),'[]'::jsonb),
    'events',coalesce((select jsonb_agg(to_jsonb(t) order by id) from public.licensing_events t),'[]'::jsonb));
end $$;
create function public.licensing_export() returns jsonb language sql security invoker set search_path='' as $$
  select licensing_private.export_register()
$$;

-- No PUBLIC function inherits an executable privilege accidentally. Private
-- privileged functions always perform current operator/session checks.
revoke all on all functions in schema licensing_private from public,anon,authenticated;
grant execute on function licensing_private.operator_id(uuid,uuid) to authenticated,service_role;
grant execute on function licensing_private.prepare(text,text,uuid,integer,text,integer,timestamptz,timestamptz,text,text),
  licensing_private.record_action(text,text,uuid,integer,text,text),licensing_private.export_register() to authenticated;
grant execute on function licensing_private.complete(uuid,uuid,uuid,text,text),
  licensing_private.import_artifact(uuid,uuid,text,text,text) to service_role;
revoke all on function public.licensing_prepare(text,text,uuid,integer,text,integer,timestamptz,timestamptz,text,text),
  public.licensing_operation(uuid),public.licensing_record_action(text,text,uuid,integer,text,text),public.licensing_export(),
  public.licensing_complete(uuid,uuid,uuid,text,text),public.licensing_import(uuid,uuid,text,text,text) from public,anon,authenticated;
grant execute on function public.licensing_prepare(text,text,uuid,integer,text,integer,timestamptz,timestamptz,text,text),
  public.licensing_operation(uuid),public.licensing_record_action(text,text,uuid,integer,text,text),public.licensing_export() to authenticated;
grant execute on function public.licensing_complete(uuid,uuid,uuid,text,text),public.licensing_import(uuid,uuid,text,text,text) to service_role;
