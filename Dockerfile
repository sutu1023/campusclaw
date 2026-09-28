FROM python:3.12-slim

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    DATABASE_PATH=/app/data/app.db \
    UPLOAD_DIR=/app/uploads

WORKDIR /app

COPY requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt

COPY app ./app
COPY scripts ./scripts
COPY run.py entrypoint.sh ./
RUN chmod +x entrypoint.sh && mkdir -p /app/data /app/uploads

EXPOSE 8080

ENTRYPOINT ["./entrypoint.sh"]
