const enc = new TextEncoder();

export type Algorithm = "MD5" | "SHA-1" | "SHA-256" | "SHA-384" | "SHA-512";

export const algorithms: Algorithm[] = ["MD5", "SHA-1", "SHA-256", "SHA-384", "SHA-512"];

export const toHex = (bytes: Uint8Array): string => Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");

export function toBase64(bytes: Uint8Array): string {
  let s = "";
  for (const b of bytes) s += String.fromCharCode(b);

  return btoa(s);
}

export function fromBase64(text: string): Uint8Array {
  const clean = text.replace(/\s/g, "").replace(/-/g, "+").replace(/_/g, "/");
  const bin = atob(clean + "=".repeat((4 - (clean.length % 4)) % 4));

  return Uint8Array.from(bin, (c) => c.charCodeAt(0));
}

/* MD5 is not in Web Crypto because it is broken for security; it is still the
 * checksum a lot of downloads publish, so it is written out here (RFC 1321). */
const S = [7, 12, 17, 22, 5, 9, 14, 20, 4, 11, 16, 23, 6, 10, 15, 21];
const K = Array.from({ length: 64 }, (_, i) => Math.floor(Math.abs(Math.sin(i + 1)) * 2 ** 32) >>> 0);

export function md5(data: Uint8Array): Uint8Array {
  const len = data.length;
  const padded = new Uint8Array(((len + 8) >> 6) * 64 + 64);
  padded.set(data);
  padded[len] = 0x80;
  const view = new DataView(padded.buffer);
  view.setUint32(padded.length - 8, (len * 8) >>> 0, true);
  view.setUint32(padded.length - 4, Math.floor((len * 8) / 2 ** 32), true);

  let a0 = 0x67452301;
  let b0 = 0xefcdab89;
  let c0 = 0x98badcfe;
  let d0 = 0x10325476;
  for (let off = 0; off < padded.length; off += 64) {
    const m = Array.from({ length: 16 }, (_, i) => view.getUint32(off + i * 4, true));
    let a = a0;
    let b = b0;
    let c = c0;
    let d = d0;
    for (let i = 0; i < 64; i++) {
      let f: number;
      let g: number;
      if (i < 16) {
        f = (b & c) | (~b & d);
        g = i;
      } else if (i < 32) {
        f = (d & b) | (~d & c);
        g = (5 * i + 1) % 16;
      } else if (i < 48) {
        f = b ^ c ^ d;
        g = (3 * i + 5) % 16;
      } else {
        f = c ^ (b | ~d);
        g = (7 * i) % 16;
      }
      f = (f + a + K[i]! + m[g]!) >>> 0;
      a = d;
      d = c;
      c = b;
      const s = S[(i >> 4) * 4 + (i % 4)]!;
      b = (b + ((f << s) | (f >>> (32 - s)))) >>> 0;
    }
    a0 = (a0 + a) >>> 0;
    b0 = (b0 + b) >>> 0;
    c0 = (c0 + c) >>> 0;
    d0 = (d0 + d) >>> 0;
  }
  const out = new Uint8Array(16);
  const ov = new DataView(out.buffer);
  [a0, b0, c0, d0].forEach((v, i) => ov.setUint32(i * 4, v, true));

  return out;
}

/** digest hashes bytes with the algorithm. */
export async function digest(alg: Algorithm, data: Uint8Array): Promise<Uint8Array> {
  if (alg === "MD5") return md5(data);

  return new Uint8Array(await crypto.subtle.digest(alg, data as BufferSource));
}

/** hmac signs bytes with a key. MD5 is built by hand over its 64-byte block. */
export async function hmac(alg: Algorithm, key: Uint8Array, data: Uint8Array): Promise<Uint8Array> {
  if (alg === "MD5") {
    const block = new Uint8Array(64);
    block.set(key.length > 64 ? md5(key) : key);
    const inner = new Uint8Array(64 + data.length);
    const outer = new Uint8Array(64 + 16);
    for (let i = 0; i < 64; i++) {
      inner[i] = block[i]! ^ 0x36;
      outer[i] = block[i]! ^ 0x5c;
    }
    inner.set(data, 64);
    outer.set(md5(inner), 64);

    return md5(outer);
  }
  const k = await crypto.subtle.importKey("raw", key as BufferSource, { name: "HMAC", hash: alg }, false, ["sign"]);

  return new Uint8Array(await crypto.subtle.sign("HMAC", k, data as BufferSource));
}

export const utf8 = (s: string): Uint8Array => enc.encode(s);

/* ---------- random ---------- */

/** randomInt returns an unbiased integer in [0, n) from the secure source. */
export function randomInt(n: number): number {
  const limit = Math.floor(2 ** 32 / n) * n;
  const buf = new Uint32Array(1);
  do {
    crypto.getRandomValues(buf);
  } while (buf[0]! >= limit);

  return buf[0]! % n;
}

export interface PasswordOptions {
  length: number;
  lower: boolean;
  upper: boolean;
  digits: boolean;
  symbols: boolean;
  /** Drops look-alike characters such as O, 0, l and 1. */
  noAmbiguous: boolean;
}

const sets = {
  lower: "abcdefghijklmnopqrstuvwxyz",
  upper: "ABCDEFGHIJKLMNOPQRSTUVWXYZ",
  digits: "0123456789",
  symbols: "!@#$%^&*()-_=+[]{};:,.?/",
};

function pools(o: PasswordOptions): string[] {
  const drop = o.noAmbiguous ? /[O0Il1|]/g : null;

  return (["lower", "upper", "digits", "symbols"] as const)
    .filter((k) => o[k])
    .map((k) => (drop ? sets[k].replace(drop, "") : sets[k]));
}

/** generatePassword draws every character from the secure source and
 *  guarantees one from each chosen class, then shuffles. */
export function generatePassword(o: PasswordOptions): string {
  const classes = pools(o);
  if (classes.length === 0) throw new Error("choose at least one character type");
  if (o.length < classes.length) throw new Error("the length is shorter than the character types chosen");
  const all = classes.join("");
  const chars = classes.map((c) => c[randomInt(c.length)]!);
  while (chars.length < o.length) chars.push(all[randomInt(all.length)]!);
  for (let i = chars.length - 1; i > 0; i--) {
    const j = randomInt(i + 1);
    [chars[i], chars[j]] = [chars[j]!, chars[i]!];
  }

  return chars.join("");
}

/** entropyBits is the size of the space the options draw from, in bits. */
export function entropyBits(o: PasswordOptions): number {
  const size = pools(o).join("").length;

  return size > 1 ? o.length * Math.log2(size) : 0;
}

/* ---------- identifiers ---------- */

const hex = (b: Uint8Array): string => toHex(b);

export function uuidV4(): string {
  return crypto.randomUUID();
}

/** uuidV7 is a time-ordered UUID (RFC 9562): 48 bits of milliseconds then random. */
export function uuidV7(now = Date.now()): string {
  const b = crypto.getRandomValues(new Uint8Array(16));
  let t = now;
  for (let i = 5; i >= 0; i--) {
    b[i] = t % 256;
    t = Math.floor(t / 256);
  }
  b[6] = (b[6]! & 0x0f) | 0x70;
  b[8] = (b[8]! & 0x3f) | 0x80;
  const h = hex(b);

  return `${h.slice(0, 8)}-${h.slice(8, 12)}-${h.slice(12, 16)}-${h.slice(16, 20)}-${h.slice(20)}`;
}

const CROCKFORD = "0123456789ABCDEFGHJKMNPQRSTVWXYZ";

export function ulid(now = Date.now()): string {
  let t = now;
  let time = "";
  for (let i = 0; i < 10; i++) {
    time = CROCKFORD[t % 32]! + time;
    t = Math.floor(t / 32);
  }
  let rand = "";
  for (let i = 0; i < 16; i++) rand += CROCKFORD[randomInt(32)]!;

  return time + rand;
}

/** decodeId returns the embedded time of a UUID v7 or ULID, or null. */
export function decodeId(id: string): { kind: string; time: Date } | null {
  const s = id.trim();
  if (/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(s)) {
    return { kind: "UUID v7", time: new Date(parseInt(s.replace(/-/g, "").slice(0, 12), 16)) };
  }
  if (/^[0-7][0-9A-HJKMNP-TV-Z]{25}$/i.test(s)) {
    let ms = 0;
    for (const c of s.slice(0, 10).toUpperCase()) ms = ms * 32 + CROCKFORD.indexOf(c);

    return { kind: "ULID", time: new Date(ms) };
  }

  return null;
}

/* ---------- JWT ---------- */

export interface Jwt {
  header: unknown;
  payload: unknown;
  signaturePresent: boolean;
  expiresAt?: Date;
  issuedAt?: Date;
  notBefore?: Date;
}

const part = (p: string): unknown => JSON.parse(new TextDecoder().decode(fromBase64(p))) as unknown;

/** decodeJwt reads a token's two JSON parts. It never checks the signature. */
export function decodeJwt(token: string): Jwt {
  const parts = token.trim().replace(/^Bearer\s+/i, "").split(".");
  if (parts.length !== 3) throw new Error("a JWT has three dot-separated parts");
  let header: unknown;
  let payload: unknown;
  try {
    header = part(parts[0]!);
    payload = part(parts[1]!);
  } catch {
    throw new Error("the header or payload is not Base64URL-encoded JSON");
  }
  const claims = (payload && typeof payload === "object" ? payload : {}) as Record<string, unknown>;
  const date = (k: string): Date | undefined => (typeof claims[k] === "number" ? new Date((claims[k] as number) * 1000) : undefined);

  return {
    header,
    payload,
    signaturePresent: parts[2]!.length > 0,
    expiresAt: date("exp"),
    issuedAt: date("iat"),
    notBefore: date("nbf"),
  };
}
