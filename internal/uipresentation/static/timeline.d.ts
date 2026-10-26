export interface Span { start: string; end: string; milestone: boolean }
export interface Range { start: string; end: string }
export type Zoom = 'day' | 'week' | 'month' | 'quarter';
export type GroupBy = 'none' | 'label' | 'status' | 'mode';
export interface HasPlan { plan?: { start?: string; end?: string; milestone?: boolean } }
export interface PlannedItem extends HasPlan { id: string; labelIds?: readonly string[]; workMode?: string }
export interface Label { id: string; name: string; color: string }
export interface Group<T> { key: string; title: string; color?: string; tasks: T[] }
export interface Bar { taskId: string; x: number; width: number; milestone: boolean; span: Span }
export interface Tick { day: string; x: number; label: string; major: boolean }
export interface Arrow { from: string; to: string; d: string; conflict: boolean }
export interface Warning { itemId: string; cycle?: readonly string[]; severity: string }

declare global {
  // A classic script also consumed as a side-effect module; no product or framework dependency.
  var WerkbordPlanning: {
    addDays(day: string, n: number): string;
    daysBetween(a: string, b: string): number;
    isDay(s: string | undefined): s is string;
    spanOf(item: HasPlan): Span | null;
    PX_PER_DAY: Record<Zoom, number>;
    ROW_HEIGHT: number;
    viewRange(spans: readonly Span[], today: string, pad?: number, min?: number): Range;
    ticks(range: Range, zoom: Zoom): Tick[];
    barFor(id: string, span: Span, range: Range, zoom: Zoom): Bar;
    barsFor(items: readonly PlannedItem[], range: Range, zoom: Zoom): Map<string, Bar>;
    firstRows(keys: readonly (string | null)[]): Map<string, number>;
    groupTasks<T extends PlannedItem>(items: readonly T[], labels: readonly Label[], by: GroupBy, presentation: {
      statusOf(item: T): string; statusTitles: Readonly<Record<string, string>>;
      statusOrder: readonly string[]; positionOf(item: T): number;
    }): Group<T>[];
    arrows(bars: ReadonlyMap<string, Bar>, rows: ReadonlyMap<string, number>, dependencies: ReadonlyMap<string, readonly string[]>): Arrow[];
    warningsByTask<W extends Warning>(warnings: readonly W[]): Map<string, W[]>;
    worst(warnings: readonly Warning[] | undefined): 'error' | 'warning' | 'info' | '';
    spanTitle(span: Span, thisYear: number): string;
    localToday(now: number): string;
    safeColor(color: unknown): string;
    inkFor(background: string): '#ffffff' | '#14130f';
  };
}
