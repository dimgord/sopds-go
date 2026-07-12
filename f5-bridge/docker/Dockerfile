# F5-TTS audiobook narrator — GPU container for cloud batch runs (e.g. SaladCloud RTX 4090).
# Bakes in f5-tts, RUAccent (separate transformers<5 env), the Russian model + vocab, and the
# pipeline. At run time it fetches the book + voice reference (env URLs) and uploads MP3s.
#
# The node provides the NVIDIA driver; the pip torch wheel bundles the CUDA runtime, so a slim
# Python base is enough. Default torch supports modern GPUs (sm_89 / RTX 4090). See ../README.md.
FROM python:3.11-slim-bookworm

ENV DEBIAN_FRONTEND=noninteractive F5_HOME=/app HF_HOME=/app/.hf
RUN apt-get update && apt-get install -y --no-install-recommends \
      ffmpeg libxml2-utils gawk curl ca-certificates xz-utils \
    && rm -rf /var/lib/apt/lists/*
RUN pip install --no-cache-dir uv

WORKDIR /app

# Two venvs, mirroring the local setup (RUAccent needs transformers<5; f5-tts wants 5.x).
RUN uv venv /app/f5env       --python 3.11 && uv pip install --python /app/f5env/bin/python f5-tts
RUN uv venv /app/ruaccent-env --python 3.11 && uv pip install --python /app/ruaccent-env/bin/python ruaccent "transformers<5"

# Russian F5 model + vocab (CC-BY-NC — personal use). ~1.3 GB baked into the image.
RUN mkdir -p /app/ru-model && \
    curl -sL "https://huggingface.co/Misha24-10/F5-TTS_RUSSIAN/resolve/main/F5TTS_v1_Base_v2/model_last_inference.safetensors" -o /app/ru-model/model_v2.safetensors && \
    curl -sL "https://huggingface.co/Misha24-10/F5-TTS_RUSSIAN/resolve/main/F5TTS_v1_Base/vocab.txt" -o /app/ru-model/vocab.txt

COPY f5-bridge/f5_daemon.py f5-bridge/ruaccent_batch.py f5-bridge/fb2-to-f5.sh /app/f5-bridge/
COPY f5-bridge/docker/entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/f5-bridge/fb2-to-f5.sh /app/entrypoint.sh

# Warm the model caches into the image (vocos + RUAccent weights) so the GPU node never waits on
# a HuggingFace download mid-run. A 3 s silent ref + one tiny CPU synth triggers the vocos fetch.
RUN head -c 144000 /dev/zero > /tmp/sil.raw && \
    ffmpeg -f s16le -ar 24000 -ac 1 -i /tmp/sil.raw /app/warm.wav -y -loglevel error && \
    echo "тест" > /app/warm.txt && \
    echo "прогрев кэша" | /app/ruaccent-env/bin/python /app/f5-bridge/ruaccent_batch.py >/dev/null 2>&1 || true; \
    printf '{"text":"прогрев","output":"/tmp/w.wav"}\n' | \
      /app/f5env/bin/python /app/f5-bridge/f5_daemon.py --ckpt /app/ru-model/model_v2.safetensors \
        --vocab /app/ru-model/vocab.txt --ref /app/warm.wav --ref-text /app/warm.txt \
        --nfe 4 --device cpu >/dev/null 2>&1 || true

ENTRYPOINT ["/app/entrypoint.sh"]
