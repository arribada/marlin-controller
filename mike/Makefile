GO111MODULE 	?= on
export GO111MODULE

GOPATH			?= $(shell go env GOPATH)

LOOPWAV ?= loopwav
STREAMAUDIO ?= streamaudio

.PHONY: all
all: build

.PHONY: clean
clean: ## clean.
clean:
	@rm ./bin/${LOOPWAV} ./bin/${STREAMAUDIO}

.PHONY: build
build: ## Build.
build: ./bin/${LOOPWAV} ./bin/${STREAMAUDIO}
	@echo ">> Building"

.PHONY: loopwav
loopwav: ## play a wave file on loop, as input for the microphone
loopwav: ./bin/${LOOPWAV}
	@echo ">> Running loopwav"
	@./bin/${LOOPWAV} -wavfile=./data/wav/jfk.wav

.PHONY: streamaudio
streamaudio: ## serve on HTTP audio from microphone
streamaudio: ./bin/${STREAMAUDIO}
	@echo ">> Running streamaudio"
	@./bin/${STREAMAUDIO} -host=:8362 -core=http://marlin-augnat1:8360 -debug=true -v=true

.PHONY: ./bin/${LOOPWAV}
./bin/${LOOPWAV}:
	@mkdir -p bin
	@go build -o ./bin/${LOOPWAV} ./cmd/loopwav/main.go

.PHONY: ./bin/${STREAMAUDIO}
./bin/${STREAMAUDIO}:
	@mkdir -p bin
	@go build -o ./bin/${STREAMAUDIO} -ldflags="-extldflags '-static'" ./cmd/streamaudio

default: ./bin/${STREAMAUDIO}
