"use client";

/**
 * Botão de alternância dark/light para a navbar.
 *
 * Enquanto o tema não hidratou (`mounted === false`) renderiza um placeholder
 * do mesmo tamanho para evitar mismatch de SSR e layout shift.
 */

import { MoonIcon, SunIcon } from "lucide-react";
import { useTheme } from "next-themes";
import { useEffect, useState } from "react";
import { Button } from "@/components/ui/button";

export function ThemeToggle() {
	const { resolvedTheme, setTheme } = useTheme();
	const [mounted, setMounted] = useState(false);

	useEffect(() => setMounted(true), []);

	if (!mounted) {
		return <div className="size-8" aria-hidden />;
	}

	const isDark = resolvedTheme === "dark";

	return (
		<Button
			type="button"
			variant="ghost"
			size="icon"
			aria-label={isDark ? "Ativar tema claro" : "Ativar tema escuro"}
			onClick={() => setTheme(isDark ? "light" : "dark")}
		>
			{isDark ? <SunIcon /> : <MoonIcon />}
		</Button>
	);
}
