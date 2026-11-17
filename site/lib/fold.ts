/** Lower case without accents, so `transicao` finds `transição` (SDD-010 section 11.1). */
export function fold(text: string): string {
	return text.normalize("NFD").replace(/\p{M}/gu, "").toLowerCase();
}
