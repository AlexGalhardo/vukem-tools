import type { Metadata } from "next";
import { JetBrains_Mono } from "next/font/google";
import { SiteHeader } from "@/components/site-header";
import { ThemeProvider } from "@/components/theme-provider";
import { Toaster } from "@/components/ui/sonner";
import "./globals.css";

// Fonte única da aplicação (monospace, estética de terminal). O mesmo recurso
// alimenta texto padrão, títulos e trechos "code" — ver globals.css, onde
// --font-heading e --font-mono reaproveitam --font-sans.
const jetbrainsMono = JetBrains_Mono({
	variable: "--font-sans",
	subsets: ["latin"],
	display: "swap",
});

export const metadata: Metadata = {
	metadataBase: new URL("https://video.galhardo.tech"),
	title: "Video Studio — Download + Cut & Crop",
	description:
		"Baixe vídeos do YouTube, Instagram, X.com e TikTok em MP4/MP3 e corte/recorte vídeos direto no navegador.",
};

export default function RootLayout({ children }: LayoutProps<"/">) {
	return (
		<html
			lang="pt-BR"
			suppressHydrationWarning
			className={`${jetbrainsMono.variable} antialiased`}
		>
			<body className="bg-background text-foreground">
				<ThemeProvider>
					<div className="flex min-h-screen flex-col">
						<SiteHeader />
						<main className="mx-auto flex w-full max-w-5xl flex-1 flex-col px-4 py-6">
							{children}
						</main>
					</div>
					<Toaster position="top-center" richColors />
				</ThemeProvider>
			</body>
		</html>
	);
}
