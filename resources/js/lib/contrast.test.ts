import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";

const css = readFileSync(new URL("../../css/app.css", import.meta.url), "utf8");

function block(selector: RegExp): string {
  const found = selector.exec(css);
  if (!found) throw new Error(`no block for ${selector}`);
  const start = css.indexOf("{", found.index) + 1;

  return css.slice(start, css.indexOf("\n}", start));
}

function token(scope: string, name: string): number[] {
  const found = new RegExp(`--color-${name}:[ ]*oklch[(]([0-9.]+)%[ ]+([0-9.]+)[ ]+([0-9.]+)[)]`).exec(scope);
  if (!found) throw new Error(`token ${name} not found`);

  return [Number(found[1]) / 100, Number(found[2]), Number(found[3])];
}

function linearRGB([L, C, h]: number[]): number[] {
  const a = C * Math.cos((h! * Math.PI) / 180);
  const b = C * Math.sin((h! * Math.PI) / 180);
  const l = (L! + 0.3963377774 * a + 0.2158037573 * b) ** 3;
  const m = (L! - 0.1055613458 * a - 0.0638541728 * b) ** 3;
  const s = (L! - 0.0894841775 * a - 1.291485548 * b) ** 3;
  const rgb = [
    4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s,
    -1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s,
    -0.0041960863 * l - 0.7034186147 * m + 1.707614701 * s,
  ];

  return rgb.map((x) => Math.min(1, Math.max(0, x)));
}

function ratio(a: number[], b: number[]): number {
  const lum = ([r, g, bl]: number[]): number => 0.2126 * r! + 0.7152 * g! + 0.0722 * bl!;
  const [x, y] = [lum(linearRGB(a)), lum(linearRGB(b))];

  return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05);
}

const themes: Record<string, RegExp> = { light: /@theme[ ]*[{]/, dark: /^[.]dark[ ]*[{]/m };

for (const [theme, selector] of Object.entries(themes)) {
  test(`every text colour reaches 4.5:1 on every surface in the ${theme} theme`, () => {
    const scope = block(selector);
    for (const text of ["ink", "ink-soft", "ink-faint", "signal"]) {
      for (const surface of ["surface", "raised", "sunken", "warn-soft"]) {
        expect(ratio(token(scope, text), token(scope, surface))).toBeGreaterThanOrEqual(4.5);
      }
    }
  });
}

for (const [theme, selector] of Object.entries(themes)) {
  test(`a status colour reaches 4.5:1 on its own tint in the ${theme} theme`, () => {
    const scope = block(selector);
    for (const status of ["ok", "warn", "bad", "signal"]) {
      expect(ratio(token(scope, status), token(scope, `${status}-soft`))).toBeGreaterThanOrEqual(4.5);
    }
  });
}
