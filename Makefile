.PHONY: build link unlink test clean

BIN_DIR := bin
BIN := $(BIN_DIR)/vox
LOCAL_BIN := $(HOME)/.local/bin/vox

build:
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN) .

link: build
	@ln -sf "$$(cd $(BIN_DIR) && pwd)/vox" $(LOCAL_BIN)
	@echo "linked → $(LOCAL_BIN)"
# A stale copy earlier in PATH would keep winning over the symlink, so say so
# rather than leaving the next `vox` run silently pointing at the old binary.
	@resolved="$$(command -v vox)"; \
	if [ "$$resolved" != "$(LOCAL_BIN)" ]; then \
		echo "warning: PATH still resolves vox to $$resolved"; \
		echo "         remove it, or move $(HOME)/.local/bin earlier in PATH"; \
	fi

unlink:
	@rm -f $(LOCAL_BIN)
	@echo "removed $(LOCAL_BIN)"

test:
	go test ./...

clean:
	rm -rf $(BIN_DIR)
