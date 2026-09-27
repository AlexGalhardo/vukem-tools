"use client";

/**
 * Navbar do app: marca à esquerda, navegação centralizada (link ativo destacado
 * com o gradiente da identidade) e o toggle de tema à direita.
 *
 * No mobile o nome "Video Studio" some — fica só o ícone `>_` à esquerda — e os
 * dois botões (Download / Cut & Crop) ficam centralizados. O toggle de tema fica
 * sempre na extrema direita, precedido (a partir do md) pelo badge de deploy.
 */

import Link from "next/link";
import { usePathname } from "next/navigation";
import { DeployBadge } from "@/components/deploy-badge";
import { ThemeToggle } from "@/components/theme-toggle";
import { cn } from "@/lib/utils";

const NAV = [
	{ href: "/", label: "Download" },
	{ href: "/cut-crop", label: "Edit" },
] as const;

export function SiteHeader() {
	const pathname = usePathname();

	return (
		<header className="border-b border-border/60 bg-background/80 backdrop-blur">
			<div className="relative mx-auto flex h-14 w-full max-w-5xl items-center justify-between gap-2 px-4">
				<Link
					href="/"
					aria-label="Video Studio — início"
					className="flex shrink-0 items-center gap-1.5 text-lg font-bold tracking-tight text-brand"
				>
					<span aria-hidden>&gt;_</span>
					<span className="hidden sm:inline">Video Studio</span>
				</Link>

				<nav className="-translate-x-1/2 absolute left-1/2 flex items-center gap-1 text-sm">
					{NAV.map((item) => {
						const active = pathname === item.href;
						return (
							<Link
								key={item.href}
								href={item.href}
								aria-current={active ? "page" : undefined}
								className={cn(
									"rounded-md px-3 py-1.5 transition-colors",
									active
										? "bg-primary font-medium text-primary-foreground"
										: "text-muted-foreground hover:bg-muted hover:text-foreground",
								)}
							>
								{item.label}
							</Link>
						);
					})}
				</nav>

				<div className="flex shrink-0 items-center gap-2">
					<DeployBadge />
					<ThemeToggle />
				</div>
			</div>
		</header>
	);
}
