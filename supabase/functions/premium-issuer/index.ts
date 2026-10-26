import {createHandler} from './handler.mjs';
Deno.serve(createHandler({
  project: Deno.env.get('SUPABASE_URL') ?? '',
  publishable: Deno.env.get('SUPABASE_ANON_KEY') ?? '',
  secret: Deno.env.get('SUPABASE_SERVICE_ROLE_KEY') ?? '',
  signingKey: Deno.env.get('PREMIUM_SIGNING_KEY_PKCS8'),
  publicKey: Deno.env.get('PREMIUM_ISSUER_PUBLIC_KEY'),
  allowedOrigins: ['https://micho8cho93.github.io'],
}));
