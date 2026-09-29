import { describe, expect, it } from "vitest";
import { moneyText, presetRange, rateText } from "@/pages/analyticsCommon";

describe("presetRange", () => {
  it("returns a 1-day window for today", () => {
    const r = presetRange("today");
    expect(r.from).toBe(r.to);
    expect(r.from).toMatch(/^\d{4}-\d{2}-\d{2}$/);
  });

  it("returns 7 calendar days for 7d", () => {
    const r = presetRange("7d");
    const days = (Date.parse(`${r.to}T12:00:00Z`) - Date.parse(`${r.from}T12:00:00Z`)) / 86400000;
    expect(days).toBe(6);
    expect(r.from < r.to).toBe(true);
  });

  it("starts MTD on the first of the month", () => {
    const r = presetRange("mtd");
    expect(r.from.endsWith("-01")).toBe(true);
    expect(r.from <= r.to).toBe(true);
  });
});

describe("moneyText", () => {
  it("formats per-currency amounts without summing", () => {
    expect(moneyText({ TZS: "10000.00", USD: "50.00" })).toContain("TZS");
    expect(moneyText({ TZS: "10000.00", USD: "50.00" })).toContain("USD");
  });

  it("renders a dash for empty input", () => {
    expect(moneyText({})).toBe("—");
    expect(moneyText(undefined)).toBe("—");
  });
});

describe("rateText", () => {
  it("formats ratios as percent", () => {
    expect(rateText(0.5)).toBe("50.0%");
  });

  it("renders a dash for nullish input", () => {
    expect(rateText(null)).toBe("—");
    expect(rateText(undefined)).toBe("—");
  });
});
