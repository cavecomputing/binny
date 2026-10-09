# The build context is the repo root, not this folder, so COPY paths start there.
# compose.yml sets that up; to build by hand: docker build -f docker/Dockerfile .
FROM python:3.12-slim

WORKDIR /app

# Install Python deps from uv.lock first (cache-friendly layer). uv is mounted for this
# step only, so it isn't left in the image.
COPY pyproject.toml uv.lock ./
RUN --mount=from=ghcr.io/astral-sh/uv:0.12.24,source=/uv,target=/bin/uv \
    uv sync --locked --no-dev --no-cache --compile-bytecode --no-python-downloads
ENV PATH="/app/.venv/bin:$PATH"

COPY app.py .
COPY binny/ binny/
COPY docker/entrypoint.sh .

EXPOSE 5000

ENV BINNY_DATA_DIR=/data
ENV PYTHONDONTWRITEBYTECODE=1
ENV PYTHONUNBUFFERED=1

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD python -c "import urllib.request; urllib.request.urlopen('http://localhost:5000/login')" || exit 1

ENTRYPOINT ["/app/entrypoint.sh"]
# One worker process, with threads: the downloads, and the lock that hands out free names, live in
# its memory. A gthread worker isn't timed out by a long upload or download. No control socket:
# nothing uses it, and it would want a home folder the app user doesn't have.
CMD ["gunicorn", "--bind", "0.0.0.0:5000", "--workers", "1", "--worker-class", "gthread", "--threads", "16", "--timeout", "120", "--no-control-socket", "app:app"]
