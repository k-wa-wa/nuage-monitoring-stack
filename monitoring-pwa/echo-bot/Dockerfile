FROM python:3.12-alpine

RUN apk upgrade --no-cache

WORKDIR /app

COPY requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt

COPY bot.py ./

USER 65532:65532
EXPOSE 8080

ENTRYPOINT ["python", "-u", "/app/bot.py"]
