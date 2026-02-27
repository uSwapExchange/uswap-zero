FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends tor && rm -rf /var/lib/apt/lists/*
COPY torrc /etc/tor/torrc
CMD ["tor", "-f", "/etc/tor/torrc"]
