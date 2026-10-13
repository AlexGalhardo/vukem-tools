"""Ponto de entrada da API FastAPI (``uvicorn app.main:app --reload --port 8000``)."""

import hmac
import logging

from fastapi import FastAPI, Request
from fastapi.exceptions import RequestValidationError
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import JSONResponse, Response
from starlette.middleware.base import RequestResponseEndpoint

from app.config import APP_VERSION, settings
from app.errors import (
	ERR_INTERNAL,
	ERR_INVALID_REQUEST,
	AppError,
	OutOfMemoryError,
	UnauthorizedError,
	classify_oserror,
)
from app.platforms import detect_platform
from app.routers import compress, downloads, edit, health, platforms

logging.basicConfig(level=logging.INFO, format="%(asctime)s | %(levelname)s | %(name)s | %(message)s")
logger = logging.getLogger("video-api")

app = FastAPI(
	title=settings.APP_NAME,
	version=APP_VERSION,
	description="Download de vídeos (YouTube, Instagram, X, TikTok) em MP4/MP3/clipes de 60s e cut & crop.",
	debug=settings.DEBUG,
	docs_url="/docs" if settings.DOCS_ENABLED else None,
	redoc_url=None,
	openapi_url="/openapi.json" if settings.DOCS_ENABLED else None,
)

app.add_middleware(
	CORSMiddleware,
	allow_origins=settings.allowed_origins_list,
	allow_credentials=False,
	allow_methods=["GET", "POST"],
	allow_headers=["Content-Type", "X-API-Key"],
	expose_headers=["Content-Disposition", "X-Clip-Count", "X-Deploy-Target"],
)

_SECURITY_HEADERS = {
	"X-Content-Type-Options": "nosniff",
	"X-Frame-Options": "DENY",
	"Referrer-Policy": "no-referrer",
}


@app.middleware("http")
async def api_key_and_headers(request: Request, call_next: RequestResponseEndpoint) -> Response:
	response: Response
	if settings.API_KEY and request.url.path.startswith("/api/") and request.method != "OPTIONS":
		provided = request.headers.get("x-api-key", "")
		if not hmac.compare_digest(provided.encode(), settings.API_KEY.encode()):
			error = UnauthorizedError()
			response = JSONResponse(status_code=error.status_code, content=error.as_body())
		else:
			response = await call_next(request)
	else:
		response = await call_next(request)
	response.headers.update(_SECURITY_HEADERS)
	response.headers["X-Deploy-Target"] = settings.deploy_target
	return response


for router in (health.router, *platforms.routers, downloads.router, edit.router, compress.router):
	app.include_router(router)


@app.exception_handler(AppError)
async def app_error_handler(request: Request, exc: AppError) -> JSONResponse:
	if exc.status_code >= 500:
		logger.error("%s (%s) em %s: %s", exc.error_type, exc.status_code, request.url.path, exc.message)
	return JSONResponse(status_code=exc.status_code, content=exc.as_body())


@app.exception_handler(RequestValidationError)
async def validation_error_handler(_request: Request, exc: RequestValidationError) -> JSONResponse:
	first = exc.errors()[0] if exc.errors() else {}
	field = ".".join(str(part) for part in first.get("loc", ())[1:]) or "corpo"
	detail = f"Requisição inválida ({field}): {first.get('msg', 'valor inválido')}."
	return JSONResponse(status_code=422, content={"detail": detail, "error_type": ERR_INVALID_REQUEST})


@app.exception_handler(Exception)
async def unhandled_exception_handler(request: Request, exc: Exception) -> JSONResponse:
	mapped: AppError | None = None
	if isinstance(exc, OSError):
		mapped = classify_oserror(exc)
	elif isinstance(exc, MemoryError):
		mapped = OutOfMemoryError()
	if mapped is not None:
		return JSONResponse(status_code=mapped.status_code, content=mapped.as_body())

	logger.exception("Erro não tratado em %s", request.url.path)
	return JSONResponse(status_code=500, content={"detail": "Erro interno no servidor.", "error_type": ERR_INTERNAL})


@app.get("/", tags=["Health"])
async def root() -> dict[str, str]:
	return {"name": settings.APP_NAME, "version": APP_VERSION, "deploy_target": settings.deploy_target}


@app.get("/api/v1/detect", tags=["Health"], summary="Descobre a plataforma de uma URL")
async def detect(url: str) -> dict[str, str | None]:
	return {"platform": detect_platform(url)}
