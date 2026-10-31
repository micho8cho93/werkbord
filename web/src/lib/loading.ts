/** A failed request does not mean its sibling requests have finished loading. */
export async function awaitAll<T extends readonly unknown[]>(requests: { [K in keyof T]: Promise<T[K]> }): Promise<T> {
  const results = await Promise.allSettled(requests);
  const failed = results.find(r => r.status === 'rejected');
  if (failed?.status === 'rejected') throw failed.reason;
  return results.map(r => (r as PromiseFulfilledResult<unknown>).value) as unknown as T;
}
