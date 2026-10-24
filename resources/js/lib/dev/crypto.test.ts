import { expect, test } from "bun:test";

import {
  decodeId,
  decodeJwt,
  digest,
  entropyBits,
  generatePassword,
  hmac,
  md5,
  toBase64,
  fromBase64,
  toHex,
  ulid,
  utf8,
  uuidV7,
} from "./crypto";

test("MD5 matches the RFC 1321 vectors", () => {
  expect(toHex(md5(utf8("")))).toBe("d41d8cd98f00b204e9800998ecf8427e");
  expect(toHex(md5(utf8("abc")))).toBe("900150983cd24fb0d6963f7d28e17f72");
  expect(toHex(md5(utf8("12345678901234567890123456789012345678901234567890123456789012345678901234567890")))).toBe(
    "57edf4a22be3c955ac49da2e2107b67a",
  );
});

test("SHA-256 and HMAC match known vectors", async () => {
  expect(toHex(await digest("SHA-256", utf8("abc")))).toBe(
    "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
  );
  expect(toHex(await hmac("SHA-256", utf8("key"), utf8("The quick brown fox jumps over the lazy dog")))).toBe(
    "f7bc83f430538424b13298e6aa6fb143ef4d59a14946175997479dbc2d1a3cd8",
  );
  expect(toHex(await hmac("MD5", utf8("key"), utf8("The quick brown fox jumps over the lazy dog")))).toBe(
    "80070713463e7749b90c2dc24911e275",
  );
});

test("Base64 round-trips bytes and reads the URL-safe form", () => {
  const bytes = Uint8Array.from([0xfb, 0xff, 0xfe, 0x00]);
  expect(toBase64(bytes)).toBe("+//+AA==");
  expect(Array.from(fromBase64("-__-AA"))).toEqual(Array.from(bytes));
});

test("a password has the length and the classes asked for", () => {
  const o = { length: 24, lower: true, upper: true, digits: true, symbols: true, noAmbiguous: true };
  const p = generatePassword(o);
  expect(p).toHaveLength(24);
  expect(p).toMatch(/[a-z]/);
  expect(p).toMatch(/[A-Z]/);
  expect(p).toMatch(/\d/);
  expect(p).toMatch(/[^A-Za-z0-9]/);
  expect(p).not.toMatch(/[O0Il1|]/);
  expect(entropyBits(o)).toBeGreaterThan(120);
});

test("a password needs a character class", () => {
  expect(() =>
    generatePassword({ length: 8, lower: false, upper: false, digits: false, symbols: false, noAmbiguous: false }),
  ).toThrow();
});

test("UUID v7 and ULID carry the time they were made", () => {
  const t = 1_700_000_000_123;
  expect(decodeId(uuidV7(t))).toEqual({ kind: "UUID v7", time: new Date(t) });
  expect(decodeId(ulid(t))).toEqual({ kind: "ULID", time: new Date(t) });
  expect(decodeId("not an id")).toBeNull();
});

test("a JWT is decoded without being verified", () => {
  const b64 = (o: unknown): string => toBase64(utf8(JSON.stringify(o))).replace(/=+$/, "").replace(/\+/g, "-").replace(/\//g, "_");
  const token = `${b64({ alg: "HS256" })}.${b64({ sub: "1", exp: 1_700_000_000 })}.sig`;
  const jwt = decodeJwt(`Bearer ${token}`);
  expect(jwt.header).toEqual({ alg: "HS256" });
  expect(jwt.expiresAt).toEqual(new Date(1_700_000_000_000));
  expect(() => decodeJwt("a.b")).toThrow();
});
