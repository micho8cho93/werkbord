export interface Span { start: string; end: string; milestone: boolean }
export interface HasPlan { plan?: { start?: string; end?: string; milestone?: boolean } }
declare global {
  var WerkbordPlanning: {
    addDays(day: string, n: number): string;
    daysBetween(a: string, b: string): number;
    isDay(s: string | undefined): s is string;
    spanOf(item: HasPlan): Span | null;
    spanTitle(span: Span, thisYear: number): string;
    localToday(now: number): string;
    safeColor(color: unknown): string;
    inkFor(background: string): '#ffffff' | '#14130f';
  };
}
