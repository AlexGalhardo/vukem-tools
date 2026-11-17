import { type ClassValue, clsx } from "clsx";
import { twMerge } from "tailwind-merge";

/** cn joins class names the way shadcn components do, and lets a later class win. */
export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs));
}
