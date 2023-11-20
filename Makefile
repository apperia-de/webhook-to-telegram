
all: docker-build-amd64 docker-save deploy connect

.PHONY: docker-build
docker-build:
	docker build -t skn/telegram_gp101bot .

.PHONY: docker-build-amd64
docker-build-amd64:
	docker build --platform linux/amd64 -t skn/telegram_gp101bot-amd64 .

.PHONY: docker-save
docker-save:
	docker save skn/telegram_gp101bot-amd64 | gzip > telegram_gp101bot-amd64.tgz

#build-multi-platform:
#	docker buildx build --platform linux/amd64,linux/arm64,linux/arm/v7 -t skn/ctrader-candle-server --output type=oci,dest=./ctrader-candle-server.tar .

.PHONY: connect
connect:
	ssh -t rancher@server.apperia.de 'cd /home/rancher/docker/services/telegram.apperia.de;bash'

.PHONY: deploy
deploy:
	scp telegram_gp101bot-amd64.tgz docker-compose.yml rancher@server.apperia.de:/home/rancher/docker/services/telegram.apperia.de
	ssh rancher@server.apperia.de 'cd /home/rancher/docker/services/telegram.apperia.de; docker load -i telegram_gp101bot-amd64.tgz;rm telegram_gp101bot-amd64.tgz'





