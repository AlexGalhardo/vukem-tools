import yt_dlp
from fastapi import APIRouter

from app.config import APP_VERSION, settings

router = APIRouter(tags=["Health"])


@router.get("/health", summary="Healthcheck (Docker, PM2, Caddy)")
async def health_check() -> dict[str, str]:
	return {
		"status": "ok",
		"version": APP_VERSION,
		"yt_dlp_version": yt_dlp.version.__version__,
		"deploy_target": settings.deploy_target,
	}
