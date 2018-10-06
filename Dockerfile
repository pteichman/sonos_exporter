FROM python:3-slim

COPY requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt

COPY sonos_exporter .

ENTRYPOINT [ "python", "./sonos_exporter" ]