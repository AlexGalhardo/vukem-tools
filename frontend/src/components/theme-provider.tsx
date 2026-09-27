"use client";

/**
 * Provider de tema (dark/light) baseado em `next-themes`.
 *
 * O dark é o tema principal/padrão (`defaultTheme="dark"`); o sistema é
 * ignorado (`enableSystem={false}`) para o toggle alternar só entre os dois.
 * A escolha é persistida automaticamente em `localStorage` pelo next-themes.
 */

import { ThemeProvider as NextThemesProvider } from "next-themes";
import type { ComponentProps } from "react";

export function ThemeProvider({
	children,
	...props
}: ComponentProps<typeof NextThemesProvider>) {
	return (
		<NextThemesProvider
			attribute="class"
			defaultTheme="dark"
			enableSystem={false}
			disableTransitionOnChange
			{...props}
		>
			{children}
		</NextThemesProvider>
	);
}
