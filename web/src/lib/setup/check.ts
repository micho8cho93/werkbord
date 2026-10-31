/** Share an in-flight check; polling cannot overwrite a newer result or end its loader. */
export function singleFlight<A extends unknown[], T>(check: (...args: A) => Promise<T>): (...args: A) => Promise<T> {
  let pending: Promise<T> | undefined;
  return (...args) => {
    if (!pending) pending = check(...args).finally(() => { pending = undefined; });
    return pending;
  };
}
