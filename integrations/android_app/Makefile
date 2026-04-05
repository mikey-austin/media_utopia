SHELL := /bin/bash

export ANDROID_HOME ?= $(HOME)/Android/Sdk
export JAVA_HOME   ?= /usr/lib/jvm/java-21-openjdk-amd64

GRADLEW := ./gradlew
ADB := $(ANDROID_HOME)/platform-tools/adb

# Device serials (override with make install-pixel PIXEL_SERIAL=xxx)
PIXEL_SERIAL ?= $(shell $(ADB) devices | grep -v emulator | grep device | head -1 | cut -f1)
EMU_SERIAL   ?= $(shell $(ADB) devices | grep emulator | head -1 | cut -f1)

.PHONY: help build debug release clean compile lint install install-pixel install-emu run run-pixel bundle

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'

compile: ## Compile debug Kotlin sources (fast check)
	$(GRADLEW) compileDebugKotlin

debug: ## Build debug APK
	$(GRADLEW) assembleDebug
	@echo ""
	@echo "APK: app/build/outputs/apk/debug/app-debug.apk"

release: ## Build release APK
	$(GRADLEW) assembleRelease
	@echo ""
	@echo "APK: app/build/outputs/apk/release/app-release-unsigned.apk"

bundle: ## Build release AAB (App Bundle)
	$(GRADLEW) bundleRelease

build: debug ## Alias for debug

install: debug ## Build and install on all connected devices
	$(GRADLEW) installDebug

install-pixel: debug ## Build and install on Pixel device
	$(ADB) -s $(PIXEL_SERIAL) install -r app/build/outputs/apk/debug/app-debug.apk

install-emu: debug ## Build and install on emulator
	$(ADB) -s $(EMU_SERIAL) install -r app/build/outputs/apk/debug/app-debug.apk

run: install ## Build, install, and launch on all connected devices
	$(ADB) shell am start -n com.mediautopia.app/.MainActivity

run-pixel: install-pixel ## Build, install, and launch on Pixel
	$(ADB) -s $(PIXEL_SERIAL) shell am start -n com.mediautopia.app/.MainActivity

lint: ## Run Android lint checks
	$(GRADLEW) lintDebug

clean: ## Remove all build artifacts
	$(GRADLEW) clean

deps: ## Show dependency tree
	$(GRADLEW) :app:dependencies --configuration debugRuntimeClasspath | head -100

check-env: ## Verify build environment
	@echo "ANDROID_HOME: $(ANDROID_HOME)"
	@echo "JAVA_HOME:    $(JAVA_HOME)"
	@$(JAVA_HOME)/bin/java -version 2>&1 | head -1
	@echo "SDK platforms: $$(ls $(ANDROID_HOME)/platforms/ 2>/dev/null || echo 'not found')"
	@echo "Build tools:   $$(ls $(ANDROID_HOME)/build-tools/ 2>/dev/null || echo 'not found')"
