-- Keep hosted issuance compatible with the customer verifier and preserve perpetual terms.
create or replace function licensing_private.prepare(p_reference text,p_kind text,p_entitlement uuid,p_expected_revision integer,
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
      or length(btrim(p_customer)) not between 1 and 256 or octet_length(p_customer)>256 then raise exception 'A new issue needs a customer and revision zero'; end if;
    insert into public.licensing_entitlements(customer,status,created_by) values(p_customer,'pending',actor_id) returning * into entitlement;
    next_revision:=1;
  else
    select * into entitlement from public.licensing_entitlements where id=p_entitlement for update;
    if not found or entitlement.status<>'active' or entitlement.current_revision is distinct from p_expected_revision then
      raise exception 'The entitlement changed or is closed; refresh before retrying'; end if;
    select * into previous from public.licensing_artifacts where entitlement_id=entitlement.id and revision=entitlement.current_revision;
    if p_seats<=(previous.claims->>'seats')::integer then raise exception 'An expansion must increase the seat allowance'; end if;
    if p_customer is not null then raise exception 'Expansion preserves the existing customer'; end if;
    if (p_expires_at is not null and ((previous.claims->>'expiresAt') is null or p_expires_at<(previous.claims->>'expiresAt')::timestamptz)) or (p_support_ends_at is not null and ((previous.claims->>'supportEndsAt') is null or p_support_ends_at<(previous.claims->>'supportEndsAt')::timestamptz)) then raise exception 'Expansion may extend dates but cannot shorten existing terms'; end if;
    p_customer:=entitlement.customer;
    p_expires_at:=coalesce(p_expires_at,(previous.claims->>'expiresAt')::timestamptz);
    p_support_ends_at:=coalesce(p_support_ends_at,(previous.claims->>'supportEndsAt')::timestamptz);
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
