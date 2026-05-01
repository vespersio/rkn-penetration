.PHONY: all geoip geosite validate validate-geoip validate-geosite clean

all: geoip geosite

geoip:
	./scripts/build-geoip.sh

geosite:
	./scripts/build-geosite.sh

validate: validate-geoip validate-geosite

validate-geoip:
	./scripts/validate-geoip.sh

validate-geosite:
	./scripts/validate-geosite.sh

clean:
	rm -rf dist
	mkdir -p dist
	touch dist/.gitkeep

