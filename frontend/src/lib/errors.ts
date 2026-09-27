/**
 * errors.ts
 * ---------
 * Traduz o `error_type` estruturado que o backend devolve (ver
 * `backend/app/errors.py`) numa mensagem específica e acionável em português.
 *
 * O backend sempre responde erros como `{ detail: string, error_type?: string }`.
 * Quando `error_type` é conhecido usamos a mensagem curada abaixo (com sugestão
 * de ação); senão caímos no `detail` do backend; e só em último caso numa
 * mensagem genérica.
 */

export type KnownErrorType =
	| "TIMEOUT"
	| "VIDEO_PROCESSING_FAILED"
	| "FILE_TOO_LARGE"
	| "OUT_OF_MEMORY"
	| "NO_DISK_SPACE"
	| "NETWORK_ERROR"
	| "DOWNLOAD_FAILED"
	| "YOUTUBE_BOT_CHECK"
	| "INVALID_MEDIA"
	| "INVALID_REQUEST"
	| "UNAUTHORIZED"
	| "NOT_FOUND"
	| "CONFLICT"
	| "SERVER_BUSY"
	| "INTERNAL_ERROR";

type ErrorCopy = { title: string; action?: string };

/** Mensagem curada por código de erro. `title` = o que aconteceu; `action` = o que fazer. */
export const ERROR_COPY: Record<KnownErrorType, ErrorCopy> = {
	TIMEOUT: {
		title: "O processamento demorou demais e foi interrompido.",
		action: "Tente um vídeo mais curto ou selecione um trecho menor.",
	},
	VIDEO_PROCESSING_FAILED: {
		title: "Falha ao processar o vídeo (ffmpeg).",
		action:
			"O arquivo pode estar corrompido ou num formato não suportado. Tente outro vídeo ou converta para MP4 antes.",
	},
	FILE_TOO_LARGE: {
		title: "O vídeo passou do limite de 1 GB.",
		action: "Baixe um trecho menor, use MP3, ou corte o vídeo antes de enviar.",
	},
	OUT_OF_MEMORY: {
		title: "O servidor ficou sem memória durante o processamento.",
		action:
			"Tente um vídeo mais curto ou em resolução menor. Vídeos longos em alta resolução podem não caber na memória do servidor.",
	},
	NO_DISK_SPACE: {
		title: "Sem espaço em disco no servidor para o arquivo temporário.",
		action: "Aguarde alguns minutos e tente de novo, ou use um vídeo menor.",
	},
	NETWORK_ERROR: {
		title: "Falha de rede ao baixar o vídeo da origem.",
		action:
			"Confira se a URL está correta e acessível e tente novamente. A plataforma de origem pode estar instável.",
	},
	DOWNLOAD_FAILED: {
		title: "Não foi possível baixar esse vídeo da origem.",
		action: "Ele pode ser privado, ter sido removido, ou exigir login. Verifique a URL.",
	},
	YOUTUBE_BOT_CHECK: {
		title: "O YouTube bloqueou o IP do servidor.",
		action:
			"Tente de novo mais tarde. O administrador precisa configurar o YOUTUBE_PROXY.",
	},
	INVALID_MEDIA: {
		title: "Não consegui ler esse arquivo de vídeo.",
		action: "Ele pode estar corrompido ou num formato não suportado.",
	},
	INVALID_REQUEST: {
		title: "Requisição inválida.",
		action: "Revise os parâmetros e tente novamente.",
	},
	UNAUTHORIZED: {
		title: "O site não está autorizado no backend.",
		action: "A API_KEY do frontend e do backend precisam ser iguais.",
	},
	NOT_FOUND: {
		title: "Esse download expirou ou não existe.",
		action: "Baixe o vídeo de novo.",
	},
	CONFLICT: {
		title: "O download ainda não terminou.",
		action: "Aguarde a barra de progresso chegar a 100%.",
	},
	SERVER_BUSY: {
		title: "O servidor está processando muitos vídeos agora.",
		action: "Tente de novo em alguns minutos.",
	},
	INTERNAL_ERROR: {
		title: "Erro interno no servidor.",
		action: "Tente novamente em instantes. Se persistir, reporte.",
	},
};

function isKnown(t: string | null | undefined): t is KnownErrorType {
	return !!t && t in ERROR_COPY;
}

/**
 * Monta a mensagem final a exibir (toast / alerta) a partir do que o backend
 * mandou. Preferência: copy curada por `error_type` > `detail` do backend >
 * fallback genérico.
 */
export function resolveErrorMessage(
	errorType: string | null | undefined,
	backendDetail?: string | null,
	fallback = "Algo deu errado. Tente novamente.",
): string {
	if (isKnown(errorType)) {
		const c = ERROR_COPY[errorType];
		return c.action ? `${c.title} ${c.action}` : c.title;
	}
	return backendDetail?.trim() || fallback;
}
