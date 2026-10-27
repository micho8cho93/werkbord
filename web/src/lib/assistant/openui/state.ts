import { BuiltinActionType } from '@openuidev/svelte-lang';

export type OpenUIFieldType = 'TextField' | 'SelectField' | 'DateField' | 'CheckboxField';
export type OpenUIFieldValue = string | boolean;
export interface OpenUIFieldState {
  value: OpenUIFieldValue;
  componentType: OpenUIFieldType;
}
export type OpenUIState = Record<string, Record<string, OpenUIFieldState>>;

const FORM_NAME = /^[A-Za-z][A-Za-z0-9_-]{0,39}$/;
const FIELD_NAME = /^[A-Za-z][A-Za-z0-9_-]{0,47}$/;
const COMPONENT_TYPES = new Set<OpenUIFieldType>(['TextField', 'SelectField', 'DateField', 'CheckboxField']);
const ID_FIELD = /(?:project|task|ticket|agent|run|label)Id$/i;
const URL_FIELD = /(?:url|uri)$/i;
const ID_VALUE = /^(?:[A-Za-z0-9][A-Za-z0-9_-]{0,119}|workspace:[A-Za-z0-9:_-]{1,64}\/[A-Za-z0-9_-]{1,64})$/;
// eslint-disable-next-line no-control-regex -- reject ASCII control characters from untrusted generated values
const CONTROL_CHARS = /[\u0000-\u0008\u000B\u000C\u000E-\u001F\u007F]/;
const MAX_FORM_COUNT = 8;
const MAX_FIELDS = 24;
const MAX_FIELD_VALUE = 500;

export type StateResult = { ok: true; state: OpenUIState } | { ok: false; error: string };

function record(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value) &&
    (Object.getPrototypeOf(value) === Object.prototype || Object.getPrototypeOf(value) === null);
}

function validDay(value: string): boolean {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return false;
  const parsed = new Date(`${value}T00:00:00.000Z`);
  return Number.isFinite(parsed.getTime()) && parsed.toISOString().slice(0, 10) === value;
}

function safeExternalURL(value: string): boolean {
  try {
    const url = new URL(value);
    const host = url.hostname.toLowerCase().replace(/\.$/, '');
    if (url.protocol !== 'https:' || url.username !== '' || url.password !== '' || host === '') return false;
    if (host.startsWith('[') || host.includes(':')) return false; // IPv6 literals are not accepted as external references.
    if (host === 'localhost' || host.endsWith('.localhost') || host.endsWith('.local') ||
        host.endsWith('.internal') || host.endsWith('.lan') || host.endsWith('.home') || host.endsWith('.test')) return false;

    const octets = host.split('.');
    if (octets.length === 1) return false; // A single-label host may resolve only on a private network.
    if (octets.length === 4 && octets.every((part) => /^\d{1,3}$/.test(part) && Number(part) <= 255)) {
      const [a, b, c] = octets.map(Number);
      if (a === 0 || a === 10 || a === 127 || a >= 224 ||
          (a === 169 && b === 254) || (a === 172 && b >= 16 && b <= 31) ||
          (a === 192 && (b === 168 || b === 0 || b === 2)) ||
          (a === 100 && b >= 64 && b <= 127) ||
          (a === 198 && (b === 18 || b === 19 || (b === 51 && c === 100))) ||
          (a === 203 && b === 0 && c === 113)) return false;
    }
    return true;
  } catch {
    return false;
  }
}

function fieldState(value: unknown): OpenUIFieldState | null {
  if (!record(value)) return null;
  if (Object.keys(value).length !== 2 || !Object.hasOwn(value, 'value') || !Object.hasOwn(value, 'componentType')) return null;
  const componentType = value.componentType;
  if (typeof componentType !== 'string' || !COMPONENT_TYPES.has(componentType as OpenUIFieldType)) return null;
  const fieldType = componentType as OpenUIFieldType;
  if (fieldType === 'CheckboxField') {
    return typeof value.value === 'boolean' ? { componentType: fieldType, value: value.value } : null;
  }
  if (typeof value.value !== 'string' || value.value.length > MAX_FIELD_VALUE || CONTROL_CHARS.test(value.value)) return null;
  if (fieldType === 'DateField' && value.value !== '' && !validDay(value.value)) return null;
  return { componentType: fieldType, value: value.value };
}

/** Validate and copy only the small, primitive state shape our registered form fields use. */
export function readOpenUIState(raw: unknown): StateResult {
  if (!record(raw)) return { ok: false, error: 'The form values could not be read. Check the interface and try again.' };
  const forms = Object.entries(raw);
  if (forms.length > MAX_FORM_COUNT) return { ok: false, error: 'This interface contains too many forms. Ask Salie to simplify it.' };

  const state: OpenUIState = {};
  for (const [formName, rawFields] of forms) {
    if (!FORM_NAME.test(formName) || !record(rawFields)) return { ok: false, error: 'The form values could not be read. Check the interface and try again.' };
    const fields = Object.entries(rawFields);
    if (fields.length > MAX_FIELDS) return { ok: false, error: 'This form contains too many fields. Ask Salie to simplify it.' };
    const cleanFields: Record<string, OpenUIFieldState> = {};
    for (const [name, rawField] of fields) {
      if (!FIELD_NAME.test(name)) return { ok: false, error: 'A form field has an invalid name. Ask Salie to rebuild the form.' };
      const field = fieldState(rawField);
      if (!field) return { ok: false, error: 'The form contains a value that Werkbord cannot use. Check the values and try again.' };
      if (typeof field.value === 'string' && field.value !== '') {
        if (ID_FIELD.test(name) && !ID_VALUE.test(field.value)) return { ok: false, error: 'A selected task or project identifier is invalid. Choose it again and try again.' };
        if (URL_FIELD.test(name) && !safeExternalURL(field.value)) return { ok: false, error: 'Enter a complete external HTTPS address without credentials or a local or private host.' };
      }
      cleanFields[name] = field;
    }
    state[formName] = cleanFields;
  }
  return { ok: true, state };
}

/** Only explicit continue-conversation actions from Werkbord's registered button can reach assistant.send. */
export type ConversationMessageResult = { ok: true; message: string } | { ok: false; error: string };

export function conversationMessage(raw: unknown): ConversationMessageResult {
  if (!record(raw) || raw.type !== BuiltinActionType.ContinueConversation) {
    return { ok: false, error: 'This interface action is not supported. Ask Salie for a new one.' };
  }
  if (!record(raw.params) || Object.keys(raw.params).length !== 0) {
    return { ok: false, error: 'This interface action contains unsupported parameters. Ask Salie for a new one.' };
  }
  if (typeof raw.humanFriendlyMessage !== 'string') {
    return { ok: false, error: 'This interface action has no clear request. Ask Salie for a new one.' };
  }
  const label = raw.humanFriendlyMessage.trim();
  if (!label || label.length > 160 || CONTROL_CHARS.test(label)) {
    return { ok: false, error: 'This interface action has an invalid label. Ask Salie for a new one.' };
  }

  let values: Record<string, OpenUIFieldValue> | undefined;
  if (raw.formName !== undefined) {
    if (typeof raw.formName !== 'string' || !FORM_NAME.test(raw.formName)) {
      return { ok: false, error: 'The selected form could not be verified. Check the interface and try again.' };
    }
    if (raw.formState !== undefined) {
      if (!record(raw.formState) || Object.keys(raw.formState).length !== 1 || !Object.hasOwn(raw.formState, raw.formName)) {
        return { ok: false, error: 'The selected form could not be verified. Check the interface and try again.' };
      }
      const allForms = readOpenUIState(raw.formState);
      if (!allForms.ok) return allForms;
      const fields = allForms.state[raw.formName];
      if (fields && Object.keys(fields).length > 0) {
        values = Object.fromEntries(Object.entries(fields).map(([name, field]) => [name, field.value]));
      }
    }
  } else if (raw.formState !== undefined) {
    if (!record(raw.formState) || Object.keys(raw.formState).length !== 0) {
      return { ok: false, error: 'This action was not connected to the form it needs. Ask Salie to rebuild it.' };
    }
  }

  const message = values
    ? `${label}\nForm values: ${JSON.stringify(values)}`
    : label;
  if (message.length > 4500) return { ok: false, error: 'The selected values are too long to send. Shorten them and try again.' };
  return { ok: true, message };
}

/** Persist only validated form state in the tab's existing transcript cache. */
export function persistedOpenUIState(raw: unknown): OpenUIState | undefined {
  const result = readOpenUIState(raw);
  return result.ok && Object.values(result.state).some((fields) => Object.keys(fields).length > 0) ? result.state : undefined;
}

export function validOpenUIFormName(value: string): boolean {
  return FORM_NAME.test(value);
}

export function validOpenUIFieldName(value: string): boolean {
  return FIELD_NAME.test(value);
}

export function validOpenUIDate(value: string): boolean {
  return validDay(value);
}
