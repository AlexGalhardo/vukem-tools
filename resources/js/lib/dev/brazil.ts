import { randomInt } from "./crypto";

const digitsOf = (s: string): string => s.replace(/\D/g, "");

/* ---------- CPF ---------- */

function cpfCheck(base: number[]): number {
  const sum = base.reduce((acc, d, i) => acc + d * (base.length + 1 - i), 0);
  const r = (sum * 10) % 11;

  return r === 10 ? 0 : r;
}

export function cpfValid(input: string): boolean {
  const s = digitsOf(input);
  if (s.length !== 11 || /^(\d)\1{10}$/.test(s)) return false;
  const d = [...s].map(Number);
  const first = cpfCheck(d.slice(0, 9));

  return first === d[9] && cpfCheck(d.slice(0, 10)) === d[10];
}

export function cpfFormat(s: string): string {
  const d = digitsOf(s);

  return `${d.slice(0, 3)}.${d.slice(3, 6)}.${d.slice(6, 9)}-${d.slice(9)}`;
}

export function cpfGenerate(): string {
  for (;;) {
    const base = Array.from({ length: 9 }, () => randomInt(10));
    if (new Set(base).size === 1) continue;
    base.push(cpfCheck(base));
    base.push(cpfCheck(base));

    return base.join("");
  }
}

/* ---------- CNPJ (numeric and alphanumeric) ---------- */

const value = (c: string): number => c.charCodeAt(0) - 48;

function cnpjCheck(chars: string): number {
  const n = chars.length;
  let sum = 0;
  for (let i = 0; i < n; i++) {
    const weight = ((n - 1 - i) % 8) + 2;
    sum += value(chars[i]!) * weight;
  }
  const r = sum % 11;

  return r < 2 ? 0 : 11 - r;
}

const cnpjClean = (s: string): string => s.toUpperCase().replace(/[^0-9A-Z]/g, "");

export function cnpjValid(input: string): boolean {
  const s = cnpjClean(input);
  if (!/^[0-9A-Z]{12}\d{2}$/.test(s) || /^(.)\1{13}$/.test(s)) return false;
  const first = cnpjCheck(s.slice(0, 12));
  if (first !== Number(s[12])) return false;

  return cnpjCheck(s.slice(0, 13)) === Number(s[13]);
}

export function cnpjFormat(input: string): string {
  const s = cnpjClean(input);

  return `${s.slice(0, 2)}.${s.slice(2, 5)}.${s.slice(5, 8)}/${s.slice(8, 12)}-${s.slice(12)}`;
}

const ALNUM = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ";

export function cnpjGenerate(alphanumeric = false): string {
  let root = "";
  for (let i = 0; i < 8; i++) root += (alphanumeric ? ALNUM : ALNUM.slice(0, 10))[randomInt(alphanumeric ? 36 : 10)]!;
  const base = `${root}0001`;
  const withFirst = base + cnpjCheck(base);

  return withFirst + cnpjCheck(withFirst);
}

/* ---------- CEP ---------- */

/** cepNormalize returns the eight digits, or null if there are not eight. */
export function cepNormalize(input: string): string | null {
  const d = digitsOf(input);

  return d.length === 8 ? d : null;
}

/* ---------- fake person ---------- */

const first = ["Ana", "Bruno", "Camila", "Daniel", "Eduarda", "Felipe", "Gabriela", "Henrique", "Isabela", "João", "Karen", "Lucas", "Mariana", "Nicolas", "Olívia", "Pedro", "Rafaela", "Sofia", "Thiago", "Vitória"];
const last = ["Almeida", "Barbosa", "Cardoso", "Dias", "Ferreira", "Gomes", "Lima", "Martins", "Nascimento", "Oliveira", "Pereira", "Ribeiro", "Santos", "Silva", "Souza", "Teixeira"];
const places: { city: string; uf: string; ddd: string; cep: string }[] = [
  { city: "São Paulo", uf: "SP", ddd: "11", cep: "01" },
  { city: "Rio de Janeiro", uf: "RJ", ddd: "21", cep: "20" },
  { city: "Belo Horizonte", uf: "MG", ddd: "31", cep: "30" },
  { city: "Curitiba", uf: "PR", ddd: "41", cep: "80" },
  { city: "Porto Alegre", uf: "RS", ddd: "51", cep: "90" },
  { city: "Salvador", uf: "BA", ddd: "71", cep: "40" },
  { city: "Recife", uf: "PE", ddd: "81", cep: "50" },
  { city: "Brasília", uf: "DF", ddd: "61", cep: "70" },
];
const streets = ["Rua das Flores", "Avenida Brasil", "Rua da Paz", "Rua Sete de Setembro", "Avenida Paulista", "Rua XV de Novembro"];

export interface Person {
  name: string;
  email: string;
  cpf: string;
  birthDate: string;
  phone: string;
  address: { street: string; number: number; city: string; state: string; cep: string };
}

const pick = <T>(list: T[]): T => list[randomInt(list.length)]!;
const pad = (n: number, w: number): string => String(n).padStart(w, "0");
const ascii = (s: string): string => s.normalize("NFD").replace(/[̀-ͯ]/g, "").toLowerCase();

/** fakePerson builds one invented person. The email uses example.com, which is
 *  reserved for documentation, so a seed row can never reach a real inbox. */
export function fakePerson(today = new Date()): Person {
  const f = pick(first);
  const l = pick(last);
  const place = pick(places);
  const year = today.getFullYear() - 18 - randomInt(50);

  return {
    name: `${f} ${l}`,
    email: `${ascii(f)}.${ascii(l)}${randomInt(900) + 100}@example.com`,
    cpf: cpfFormat(cpfGenerate()),
    birthDate: `${year}-${pad(randomInt(12) + 1, 2)}-${pad(randomInt(28) + 1, 2)}`,
    phone: `(${place.ddd}) 9${pad(randomInt(10000), 4)}-${pad(randomInt(10000), 4)}`,
    address: {
      street: pick(streets),
      number: randomInt(2000) + 1,
      city: place.city,
      state: place.uf,
      cep: `${place.cep}${pad(randomInt(1000), 3)}-${pad(randomInt(1000), 3)}`,
    },
  };
}
