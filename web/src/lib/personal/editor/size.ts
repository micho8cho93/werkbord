/** The size a document will take as stored text: what the field's limit counts. */
export function byteLength(text: string): number { return new TextEncoder().encode(text).length; }
