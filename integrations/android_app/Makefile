SHELL := /bin/bash

export ANDROID_HOME ?= $(HOME)/Android/Sdk
export JAVA_HOME   ?= /usr/lib/jvm/java-21-openjdk-amd64

GRADLEW := ./gradlew

.PHONY: help build debug release clean compile lint install run bundle

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

install: debug ## Build and install debug APK on connected device
	$(GRADLEW) installDebug

run: install ## Build, install, and launch on connected device
	adb shell am start -n com.mediautopia.app/.MainActivity

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
