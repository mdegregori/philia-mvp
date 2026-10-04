(
echo # Philia Economic Protocol - Makefile
echo.
echo GO = go
echo ALICE_DIR = Alice
echo BOB_DIR = Bob
echo.
echo .PHONY: all
echo all: build
echo.
echo .PHONY: build
echo build:
echo 	@echo Compiling Philia nodes...
echo 	@$(GO) build -v -o $(ALICE_DIR)/pep_node ./$(ALICE_DIR)
echo 	@$(GO) build -v -o $(BOB_DIR)/pep_node ./$(BOB_DIR)
echo.
echo .PHONY: run-alice
echo run-alice:
echo 	@echo Starting Alice on port 4001...
echo 	@cd $(ALICE_DIR) ^&^& $(GO) run . -port 4001 -data-dir data
echo.
echo .PHONY: run-bob
echo run-bob:
echo 	@echo Starting Bob on port 4002...
echo 	@cd $(BOB_DIR) ^&^& $(GO) run . -port 4002 -data-dir data
echo.
echo .PHONY: clean
echo clean:
echo 	@echo Cleaning...
echo 	@-del /F /Q $(ALICE_DIR)\pep_node.exe 2^>nul
echo 	@-del /F /Q $(BOB_DIR)\pep_node.exe 2^>nul
echo 	@-rmdir /S /Q $(ALICE_DIR)\data 2^>nul
echo 	@-rmdir /S /Q $(BOB_DIR)\data 2^>nul
) > Makefile