import { describe, it, expect } from "vitest";
import {
  instantFromWall,
  wallTime,
  editableOrchestration,
  emptyOrchestration,
} from "./scheduling";
describe("IANA wall times", () => {
  it("keeps explicit offsets safe across zones", () => {
    expect(instantFromWall("2026-10-04T01:00", "Europe/Madrid")).toBe(
      "2026-10-03T23:00:00.000Z",
    );
    expect(wallTime("2026-10-03T23:00Z", "Europe/Madrid")).toBe(
      "2026-10-04T01:00",
    );
  });
  it("rejects nonexistent spring-forward times", () => {
    expect(() => instantFromWall("2026-03-29T02:30", "Europe/Madrid")).toThrow(
      "does not exist",
    );
  });
  it("disambiguates repeated fall-back times", () => {
    expect(
      instantFromWall("2026-10-25T02:30", "Europe/Madrid", "earlier"),
    ).toBe("2026-10-25T00:30:00.000Z");
    expect(instantFromWall("2026-10-25T02:30", "Europe/Madrid", "later")).toBe(
      "2026-10-25T01:30:00.000Z",
    );
  });
  it("handles fractional-offset zones", () => {
    expect(instantFromWall("2026-10-04T01:00", "Asia/Kathmandu")).toBe(
      "2026-10-03T19:15:00.000Z",
    );
  });
  it("rejects invalid zones", () =>
    expect(() => instantFromWall("2026-10-04T01:00", "invalid")).toThrow());
  it("clears empty date input", () =>
    expect(instantFromWall("", "UTC")).toBeUndefined());
  it("strips controller-owned dispatch metadata when editing", () => {
    const o = {
      ...emptyOrchestration(),
      key: "old",
      runId: "run",
      missed: true,
      error: "old",
    };
    expect(editableOrchestration(o)).not.toHaveProperty("runId");
    expect(editableOrchestration(o)).not.toHaveProperty("key");
  });
});
